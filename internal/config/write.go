package config

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/v2"
)

const (
	dirMode  = 0o750
	fileMode = 0o600
	// ResetAll is the argument of Reset that clears every key.
	ResetAll = "all"
)

// Set validates key=raw against the schema and rewrites the YAML file atomically. Comments
// in an existing file are not preserved, there is no lock between concurrent writers (the last
// one wins) and the rename replaces a symlinked file with a regular one. The file is created
// when missing.
func Set(path, key, raw string) error {
	s, ok := lookup(key)
	if !ok {
		return &Error{Kind: KindUnknownKey, Key: key, Reason: "unknown configuration key"}
	}
	v, err := s.parse(raw)
	if err != nil {
		return &Error{Kind: KindInvalidValue, Key: key, Value: raw, Reason: err.Error(), Err: err}
	}
	flat, err := readFlat(path)
	if err != nil {
		return err
	}
	next := maps.Clone(flat)
	next[key] = s.out(v)
	return writeFlat(path, next)
}

// Reset removes a key, a whole section (for example cache.ttl) or, with ResetAll, every key
// from the file. The file is created when missing, holding only the schema version.
func Reset(path, key string) error {
	if key != ResetAll {
		if _, ok := lookup(key); !ok && !isGroup(key) {
			return &Error{Kind: KindUnknownKey, Key: key, Reason: "unknown configuration key"}
		}
	}
	flat, err := readFlat(path)
	if err != nil {
		return err
	}
	next := make(map[string]any, len(flat))
	for k, v := range flat {
		if key != ResetAll && k != key && !strings.HasPrefix(k, key+".") {
			next[k] = v
		}
	}
	return writeFlat(path, next)
}

// readFlat loads the existing file as flat leaves; a missing file is empty. The schema
// version key is dropped here and rewritten on save.
func readFlat(path string) (map[string]any, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: the config path is chosen by the user (flag, env, default)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, &Error{Kind: KindUnreadable, Source: SourceFile, Reason: "cannot read " + path, Err: err}
	}
	flat, err := parseYAML(data)
	if err != nil {
		return nil, &Error{Kind: KindUnreadable, Source: SourceFile, Reason: "invalid YAML in " + path + "; refusing to overwrite it", Err: err}
	}
	if raw, ok := flat["version"]; ok {
		if verr := checkVersion(raw); verr != nil {
			return nil, verr
		}
	}
	delete(flat, "version")
	return flat, nil
}

func writeFlat(path string, flat map[string]any) error {
	out := maps.Clone(flat)
	out["version"] = SchemaVersion
	k := koanf.New(".")
	if err := k.Load(confmap.Provider(out, "."), nil); err != nil {
		return fmt.Errorf("config: build document: %w", err)
	}
	data, err := k.Marshal(yaml.Parser())
	if err != nil {
		return fmt.Errorf("config: encode YAML: %w", err)
	}
	if err := atomicWrite(path, data); err != nil {
		return &Error{Kind: KindUnreadable, Source: SourceFile, Reason: "cannot write " + path, Err: err}
	}
	return nil
}

// atomicWrite writes to a temp file in the same directory, syncs and renames over path.
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".mothdex-config-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if err := writeAndSync(tmp, data); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Chmod(name, fileMode); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}

func writeAndSync(f *os.File, data []byte) error {
	_, werr := f.Write(data)
	serr := f.Sync()
	cerr := f.Close()
	return errors.Join(werr, serr, cerr)
}
