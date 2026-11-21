package cache

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	dirMode      = 0o700
	metaSuffix   = ".meta.json"
	tmpPrefix    = ".tmp-"
	metaVersion  = 1
	touchEvery   = time.Minute // last access is refreshed at most once per minute per item
	maxSmallItem = 4 << 20     // limit for session and state items
	staleTmpAge  = time.Hour
)

// nsInfo describes how a namespace is stored.
type nsInfo struct {
	evictable bool // counts toward the cap and is removed by Clear without arguments
	binary    bool // .bin data instead of .json
}

var namespaces = map[string]nsInfo{
	NSSearch:  {evictable: true},
	NSEntry:   {evictable: true},
	NSMeta:    {evictable: true},
	NSInfo:    {evictable: true},
	NSCovers:  {evictable: true, binary: true},
	NSSession: {},
	NSState:   {},
}

// evictableNamespaces lists the namespaces under the cap, in a fixed order.
var evictableNamespaces = []string{NSSearch, NSEntry, NSMeta, NSInfo, NSCovers}

var allNamespaces = append(append([]string{}, evictableNamespaces...), NSSession, NSState)

func namespaceInfo(ns string) (nsInfo, error) {
	info, ok := namespaces[ns]
	if !ok {
		return nsInfo{}, fmt.Errorf("%w: %q", ErrUnknownNamespace, ns)
	}
	return info, nil
}

func (s *store) dir(ns string) string { return filepath.Join(s.root, ns) }

func (s *store) paths(ns, key string, info nsInfo) (data, meta string) {
	ext := ".json"
	if info.binary {
		ext = ".bin"
	}
	return filepath.Join(s.dir(ns), key+ext), filepath.Join(s.dir(ns), key+metaSuffix)
}

func readMeta(path string) (Meta, error) {
	raw, err := readShared(path)
	if err != nil {
		return Meta{}, err
	}
	var m Meta
	if err := json.Unmarshal(raw, &m); err != nil {
		return Meta{}, fmt.Errorf("corrupt sidecar: %w", err)
	}
	return m, nil
}

func (m Meta) expired(now time.Time) bool { return m.ExpiresAt != nil && !now.Before(*m.ExpiresAt) }

// valid checks a sidecar against its location and the data file size.
func (m Meta) valid(ns string, dataSize int64) bool {
	return m.V == metaVersion && m.NS == ns && m.Size == dataSize
}

// writeTemp copies r into a new temp file of dir, refusing more than limit bytes. The
// temp file is removed on any error.
func writeTemp(dir string, r io.Reader, limit int64, mtime time.Time) (path string, n int64, err error) {
	f, err := os.CreateTemp(dir, tmpPrefix+"*")
	if err != nil {
		return "", 0, err
	}
	path = f.Name()
	n, err = io.Copy(f, io.LimitReader(r, limit+1))
	if err == nil && n > limit {
		err = ErrTooLarge
	}
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chtimes(path, mtime, mtime)
	}
	if err != nil {
		_ = os.Remove(path)
		return "", 0, err
	}
	return path, n, nil
}

const (
	renameAttempts = 8
	renameBackoff  = 5 * time.Millisecond
)

// readShared reads a whole cache file through openShared, so the read never blocks a rename over it.
func readShared(path string) ([]byte, error) {
	f, err := openShared(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return io.ReadAll(f)
}

// replaceFile renames src over dst. On Windows a file that a reader has open cannot be
// replaced and the rename fails with "access denied" for a moment, so it is retried briefly
// (a few milliseconds of real time; the cache is best effort and a failed Put is not fatal).
func replaceFile(src, dst string) error {
	var err error
	for attempt := 0; attempt < renameAttempts; attempt++ {
		if err = renameOver(src, dst); err == nil || runtime.GOOS != "windows" {
			return err
		}
		time.Sleep(time.Duration(attempt+1) * renameBackoff)
	}
	return err
}

// itemInfo is what a directory scan learns about one cached item.
type itemInfo struct {
	ns, key string
	bytes   int64 // data plus sidecar
	mtime   time.Time
	expired bool
	corrupt bool // missing or unreadable sidecar, missing data, or size mismatch
}

// scan lists the items of one namespace, including orphans.
func (s *store) scan(ns string, now time.Time) ([]itemInfo, error) {
	info, err := namespaceInfo(ns)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(s.dir(ns))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	keys := map[string]bool{}
	for _, e := range entries {
		if key, ok := itemKey(e.Name(), info); ok {
			keys[key] = true
		}
	}
	out := make([]itemInfo, 0, len(keys))
	for key := range keys {
		out = append(out, s.inspect(ns, key, info, now))
	}
	return out, nil
}

// itemKey extracts the key from a data or sidecar file name; temp and foreign files give false.
func itemKey(name string, info nsInfo) (string, bool) {
	if strings.HasPrefix(name, tmpPrefix) {
		return "", false
	}
	if key, ok := strings.CutSuffix(name, metaSuffix); ok {
		return key, validKey(key) == nil
	}
	ext := ".json"
	if info.binary {
		ext = ".bin"
	}
	key, ok := strings.CutSuffix(name, ext)
	return key, ok && validKey(key) == nil
}

func (s *store) inspect(ns, key string, info nsInfo, now time.Time) itemInfo {
	dataPath, metaPath := s.paths(ns, key, info)
	it := itemInfo{ns: ns, key: key}
	var dataSize int64
	dst, derr := os.Stat(dataPath)
	if derr == nil {
		dataSize, it.mtime = dst.Size(), dst.ModTime()
	}
	mst, merr := os.Stat(metaPath)
	if merr == nil {
		it.bytes = mst.Size()
	}
	it.bytes += dataSize
	meta, rerr := readMeta(metaPath)
	if derr != nil || merr != nil || rerr != nil || !meta.valid(ns, dataSize) {
		it.corrupt = true
		return it
	}
	it.expired = meta.expired(now)
	return it
}

// removeFiles deletes the data and sidecar of an item; missing files are fine.
func (s *store) removeFiles(ns, key string, info nsInfo) error {
	dataPath, metaPath := s.paths(ns, key, info)
	return errors.Join(removeIfExists(dataPath), removeIfExists(metaPath))
}

func removeIfExists(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// itemBytes returns the bytes an item occupies now (0 when absent).
func (s *store) itemBytes(ns, key string, info nsInfo) int64 {
	dataPath, metaPath := s.paths(ns, key, info)
	var total int64
	for _, p := range []string{dataPath, metaPath} {
		if st, err := os.Stat(p); err == nil {
			total += st.Size()
		}
	}
	return total
}
