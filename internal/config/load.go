package config

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"runtime"
	"slices"
	"sort"
	"strings"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/rawbytes"
	"github.com/knadh/koanf/v2"
)

// Options are the inputs of Load. Everything the process environment would supply is passed
// in, so tests need no real environment.
type Options struct {
	File     string                       // config file path; "" = no file
	ReadFile func(string) ([]byte, error) // default os.ReadFile
	Environ  []string                     // KEY=VALUE entries, as os.Environ()
	Flags    map[string]string            // config key -> raw value from command flags (this run only)
	Force    bool                         // --force was passed: download.on_exists=overwrite may come from file or env
}

// Result is the outcome of Load: the typed config plus where each value came from.
type Result struct {
	Config   Config
	Warnings []string
	vals     map[string]any
	src      map[string]Source
	order    []string // keys in list order
}

// Load merges defaults < file < env < flags per key and validates every layer.
func Load(o Options) (Result, error) {
	fileVals, warnings, err := fileLayer(o)
	if err != nil {
		return Result{}, err
	}
	envVals, envWarnings := envLayer(o.Environ, runtime.GOOS == "windows")
	flagVals := make(map[string]any, len(o.Flags))
	for k, v := range o.Flags {
		flagVals[k] = v
	}
	st := newState()
	for _, l := range []struct {
		src  Source
		vals map[string]any
	}{{SourceFile, fileVals}, {SourceEnv, envVals}, {SourceFlag, flagVals}} {
		if err := st.apply(l.src, l.vals); err != nil {
			return Result{}, err
		}
	}
	if err := st.checkOverwrite(o.Force); err != nil {
		return Result{}, err
	}
	return st.result(slices.Concat(warnings, envWarnings, st.warnings)), nil
}

// checkOverwrite refuses download.on_exists=overwrite that a file or the environment supplies
// when --force is absent (case 22): a persistent setting must not silently destroy files.
func (st *state) checkOverwrite(force bool) error {
	const key = "download.on_exists"
	src := st.src[key]
	if force || st.vals[key] != "overwrite" || (src != SourceFile && src != SourceEnv) {
		return nil
	}
	return &Error{Kind: KindInvalidValue, Key: key, Value: "overwrite", Source: src,
		Reason: "overwrite from the config file or environment needs --force"}
}

// state accumulates the effective value and source per key.
type state struct {
	vals     map[string]any
	src      map[string]Source
	warnings []string
}

func newState() *state {
	st := &state{vals: map[string]any{}, src: map[string]Source{}}
	for _, s := range schema {
		st.vals[s.key()] = s.defaultValue()
		st.src[s.key()] = SourceDefault
	}
	return st
}

// apply validates every value of one layer (in key order) and lets it override lower layers.
func (st *state) apply(src Source, vals map[string]any) error {
	for _, key := range slices.Sorted(maps.Keys(vals)) {
		s, ok := lookup(key)
		if !ok {
			return &Error{Kind: KindUnknownKey, Key: key, Source: src, Reason: "unknown configuration key"}
		}
		v, err := s.parse(vals[key])
		if err != nil {
			return valueError(key, vals[key], src, err)
		}
		if w := clampWarning(key, vals[key], v); w != "" {
			st.warnings = append(st.warnings, w)
		}
		st.vals[key], st.src[key] = v, src
	}
	return nil
}

// clampWarning reports a value that parse silently moved into range (api.rate_limit only).
func clampWarning(key string, raw, parsed any) string {
	if key != "api.rate_limit" {
		return ""
	}
	if f, err := asFloat(raw); err == nil && f != parsed.(float64) {
		return fmt.Sprintf("api.rate_limit %v is outside 1..10 and was clamped to %v", f, parsed)
	}
	return ""
}

func (st *state) result(warnings []string) Result {
	var cfg Config
	order := make([]string, 0, len(st.vals))
	for _, s := range schema {
		s.bind(&cfg, st.vals[s.key()])
		order = append(order, s.key())
	}
	cfg.Profile.Folders = map[string]string{}
	var folderKeys []string
	for key := range st.vals {
		if strings.HasPrefix(key, folderPrefix) {
			folderKeys = append(folderKeys, key)
		}
	}
	sort.Strings(folderKeys)
	for _, key := range folderKeys {
		s, _ := lookup(key)
		s.bind(&cfg, st.vals[key])
	}
	return Result{
		Config:   cfg,
		Warnings: warnings,
		vals:     maps.Clone(st.vals),
		src:      maps.Clone(st.src),
		order:    append(order, folderKeys...),
	}
}

// fileLayer reads and parses the YAML file into flat key -> raw value. A missing file is fine.
func fileLayer(o Options) (map[string]any, []string, error) {
	if o.File == "" {
		return map[string]any{}, nil, nil
	}
	read := o.ReadFile
	if read == nil {
		read = os.ReadFile
	}
	data, err := read(o.File)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]any{}, nil, nil
	}
	if err != nil {
		return nil, nil, &Error{Kind: KindUnreadable, Source: SourceFile, Reason: "cannot read " + o.File, Err: err}
	}
	flat, err := parseYAML(data)
	if err != nil {
		return nil, nil, &Error{Kind: KindUnreadable, Source: SourceFile, Reason: "invalid YAML in " + o.File, Err: err}
	}
	return filterFile(flat)
}

// parseYAML returns the flat (dotted key) leaves of a YAML document; empty maps are dropped.
func parseYAML(data []byte) (map[string]any, error) {
	k := koanf.New(".")
	if err := k.Load(rawbytes.Provider(data), yaml.Parser()); err != nil {
		return nil, err
	}
	flat := k.All()
	for key, v := range flat {
		if m, isMap := v.(map[string]any); isMap && len(m) == 0 {
			delete(flat, key)
		}
	}
	return flat, nil
}

// filterFile checks the schema version, drops unknown keys with a warning and rejects a
// non-map value where the folders map belongs.
func filterFile(flat map[string]any) (map[string]any, []string, error) {
	if raw, ok := flat["version"]; ok {
		if err := checkVersion(raw); err != nil {
			return nil, nil, err
		}
	}
	out := make(map[string]any, len(flat))
	var warnings []string
	for _, key := range slices.Sorted(maps.Keys(flat)) {
		switch _, known := lookup(key); {
		case key == "version":
		case key == strings.TrimSuffix(folderPrefix, "."):
			return nil, nil, &Error{Kind: KindInvalidValue, Key: key, Value: fmt.Sprint(flat[key]), Source: SourceFile, Reason: "must be a map of platform id to folder"}
		case known:
			out[key] = flat[key]
		default:
			warnings = append(warnings, fmt.Sprintf("unknown key %q in config file ignored", key))
		}
	}
	return out, warnings, nil
}

func checkVersion(raw any) error {
	n, err := asInt(raw)
	if err != nil || n < 1 {
		return &Error{Kind: KindInvalidValue, Key: "version", Value: fmt.Sprint(raw), Source: SourceFile, Reason: "must be a positive whole number"}
	}
	if n > SchemaVersion {
		return &Error{Kind: KindNewerSchema, Key: "version", Value: fmt.Sprint(raw), Source: SourceFile,
			Reason: fmt.Sprintf("config file schema %d is newer than the supported %d; upgrade mothdex", n, SchemaVersion)}
	}
	return nil
}

// Setting is one effective value for `config list` and `config get`.
type Setting struct {
	Key    string
	Value  any // bool, int, float64, string (durations and sizes as text) or []string
	Source Source
}

// Settings lists every effective value in schema order, folder overrides last.
func (r Result) Settings() []Setting {
	out := make([]Setting, 0, len(r.order))
	for _, key := range r.order {
		out = append(out, r.setting(key))
	}
	return out
}

// Get returns one effective value; an unknown key is a KindUnknownKey error.
func (r Result) Get(key string) (Setting, error) {
	if _, ok := r.vals[key]; ok {
		return r.setting(key), nil
	}
	if s, ok := lookup(key); ok { // a valid key nobody set, such as a profile.folders.<id> entry
		return Setting{Key: key, Value: s.out(s.defaultValue()), Source: SourceDefault}, nil
	}
	return Setting{}, &Error{Kind: KindUnknownKey, Key: key, Reason: "unknown configuration key"}
}

func (r Result) setting(key string) Setting {
	s, _ := lookup(key)
	return Setting{Key: key, Value: s.out(r.vals[key]), Source: r.src[key]}
}
