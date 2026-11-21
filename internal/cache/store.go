package cache

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	maxItemShareDivisor = 4 // one item may use at most a quarter of the cap
	metaFileMode        = 0o600
)

// store is the on-disk implementation of Store.
type store struct {
	root  string
	cfg   Config
	clock Clock
	log   *slog.Logger

	mu    sync.Mutex // serializes this process's writes and guards total
	total int64      // bytes of evictable namespaces, corrected by Prune
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// Open prepares root (usually <paths.temp>/cache) and measures the evictable namespaces
// with one directory scan. The zero Config has Enabled=false: evictable namespaces are off
// until the caller sets Enabled.
func Open(root string, cfg Config, clock Clock, log *slog.Logger) (Store, error) {
	if root == "" {
		return nil, errors.New("cache: empty root")
	}
	if cfg.MaxBytes <= 0 {
		cfg.MaxBytes = DefaultMaxBytes
	}
	if clock == nil {
		clock = realClock{}
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	if err := os.MkdirAll(root, dirMode); err != nil {
		return nil, fmt.Errorf("cache: create root: %w", err)
	}
	s := &store{root: root, cfg: cfg, clock: clock, log: log}
	total, err := s.measure()
	if err != nil {
		return nil, fmt.Errorf("cache: scan: %w", err)
	}
	s.total = total
	return s, nil
}

// ttlFor returns the lifetime of an item and whether it may be stored at all.
func (s *store) ttlFor(ns string, info nsInfo, o putOpts) (ttl time.Duration, ok bool) {
	if !info.evictable {
		return o.ttl, true // session and state: no expiry unless the item asks for one
	}
	if !s.cfg.Enabled {
		return 0, false
	}
	ttl = map[string]time.Duration{
		NSSearch: s.cfg.TTL.Search, NSEntry: s.cfg.TTL.Entry, NSMeta: s.cfg.TTL.Meta,
		NSInfo: s.cfg.TTL.Info, NSCovers: s.cfg.TTL.Covers,
	}[ns]
	if ttl <= 0 {
		return 0, false // TTL 0 turns the namespace off, even for an item that asks for a TTL
	}
	if o.ttl > 0 {
		ttl = o.ttl
	}
	return ttl, true
}

func (s *store) readable(ns string, info nsInfo) bool {
	if !info.evictable {
		return true
	}
	_, ok := s.ttlFor(ns, info, putOpts{})
	return ok
}

// check validates the namespace and key and the kind of data the caller wants.
func check(ns, key string, wantBinary bool) (nsInfo, error) {
	info, err := namespaceInfo(ns)
	if err != nil {
		return nsInfo{}, err
	}
	if err := validKey(key); err != nil {
		return nsInfo{}, err
	}
	if info.binary != wantBinary {
		return nsInfo{}, fmt.Errorf("%w: %q", ErrWrongKind, ns)
	}
	return info, nil
}

// lookup finds a live item: sidecar valid, data size matching, not expired (or stale within
// the allowance). Corrupt items are deleted and reported as a miss.
func (s *store) lookup(ns, key string, info nsInfo, o getOpts) (dataPath string, e Entry) {
	if !s.readable(ns, info) {
		return "", Entry{}
	}
	dataPath, metaPath := s.paths(ns, key, info)
	now := s.clock.Now()
	dst, derr := os.Stat(dataPath)
	meta, merr := readMeta(metaPath)
	if derr != nil || merr != nil || !meta.valid(ns, dst.Size()) {
		if !errors.Is(derr, fs.ErrNotExist) || !errors.Is(merr, fs.ErrNotExist) {
			s.discard(ns, key, info, "corrupt or incomplete item", func() bool { return !s.intact(ns, key, info) })
		}
		return "", Entry{}
	}
	e = Entry{Hit: true, Meta: meta}
	if meta.expired(now) {
		if o.staleFor <= 0 || now.Sub(*meta.ExpiresAt) > o.staleFor {
			return "", Entry{}
		}
		e.Stale = true
	}
	if now.Sub(dst.ModTime()) >= touchEvery {
		if err := os.Chtimes(dataPath, now, now); err != nil {
			s.log.Debug("cache: cannot refresh last access", "ns", ns, "err", err)
		}
	}
	return dataPath, e
}

// intact reports whether the item on disk has a valid sidecar and matching data size.
func (s *store) intact(ns, key string, info nsInfo) bool {
	dataPath, metaPath := s.paths(ns, key, info)
	dst, derr := os.Stat(dataPath)
	meta, merr := readMeta(metaPath)
	return derr == nil && merr == nil && meta.valid(ns, dst.Size())
}

// discard deletes a bad item and corrects the size total. A reader can see a half-written
// state while a writer holds the lock between its two renames, so the verdict is re-checked
// under the lock (stillBad) and a freshly written item is never removed.
func (s *store) discard(ns, key string, info nsInfo, why string, stillBad func() bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !stillBad() {
		return
	}
	freed := s.itemBytes(ns, key, info)
	if err := s.removeFiles(ns, key, info); err != nil {
		s.log.Debug("cache: cannot delete item", "ns", ns, "err", err)
		return
	}
	if info.evictable {
		s.total = max(s.total-freed, 0)
	}
	s.log.Debug("cache: item discarded", "ns", ns, "reason", why)
}

// GetJSON reads and decodes a JSON item.
func (s *store) GetJSON(ns, key string, v any, opts ...GetOption) (Entry, error) {
	info, err := check(ns, key, false)
	if err != nil {
		return Entry{}, err
	}
	path, e := s.lookup(ns, key, info, applyGet(opts))
	if !e.Hit {
		return Entry{}, nil
	}
	raw, err := readShared(path)
	if err == nil {
		err = json.Unmarshal(raw, v)
	}
	if err != nil {
		s.discard(ns, key, info, "undecodable JSON", func() bool { return !s.decodes(ns, key, info) })
		return Entry{}, nil
	}
	return e, nil
}

// GetBlob opens a binary item; the caller closes it. A miss returns a nil reader.
func (s *store) GetBlob(ns, key string, opts ...GetOption) (io.ReadCloser, Entry, error) {
	info, err := check(ns, key, true)
	if err != nil {
		return nil, Entry{}, err
	}
	path, e := s.lookup(ns, key, info, applyGet(opts))
	if !e.Hit {
		return nil, Entry{}, nil
	}
	f, err := openShared(path)
	if err != nil {
		s.discard(ns, key, info, "unopenable data", func() bool { return !s.intact(ns, key, info) })
		return nil, Entry{}, nil
	}
	return f, e, nil
}

// decodes reports whether the data file currently holds valid JSON (and the item is intact).
func (s *store) decodes(ns, key string, info nsInfo) bool {
	dataPath, _ := s.paths(ns, key, info)
	raw, err := readShared(dataPath)
	return err == nil && s.intact(ns, key, info) && json.Valid(raw)
}

func applyGet(opts []GetOption) getOpts {
	var o getOpts
	for _, f := range opts {
		f(&o)
	}
	return o
}

func applyPut(opts []PutOption) putOpts {
	var o putOpts
	for _, f := range opts {
		f(&o)
	}
	return o
}

// PutJSON stores v as JSON. ErrTooLarge means the item exceeds a quarter of the cap.
func (s *store) PutJSON(ns, key string, v any, opts ...PutOption) error {
	info, err := check(ns, key, false)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("cache: encode: %w", err)
	}
	return s.put(ns, key, info, bytes.NewReader(raw), "", int64(len(raw)), applyPut(opts))
}

// PutBlob stores up to maxBytes read from r. ErrTooLarge means the body is over maxBytes or
// over a quarter of the cap; nothing is stored then.
func (s *store) PutBlob(ns, key string, r io.Reader, contentType string, maxBytes int64, opts ...PutOption) error {
	info, err := check(ns, key, true)
	if err != nil {
		return err
	}
	return s.put(ns, key, info, r, contentType, maxBytes, applyPut(opts))
}

func (s *store) put(ns, key string, info nsInfo, r io.Reader, contentType string, limit int64, o putOpts) error {
	ttl, ok := s.ttlFor(ns, info, o)
	if !ok {
		return nil
	}
	if info.evictable {
		limit = min(limit, s.cfg.MaxBytes/maxItemShareDivisor)
	} else {
		limit = min(limit, maxSmallItem)
	}
	if err := os.MkdirAll(s.dir(ns), dirMode); err != nil {
		return fmt.Errorf("cache: create namespace: %w", err)
	}
	now := s.clock.Now()
	// The (possibly slow) copy from r happens outside the lock; only the renames, the
	// sidecar and the size total need it.
	tmp, n, err := writeTemp(s.dir(ns), r, limit, now)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	prev := s.itemBytes(ns, key, info)
	dataPath, metaPath := s.paths(ns, key, info)
	if err := replaceFile(tmp, dataPath); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("cache: store data: %w", err)
	}
	metaSize, err := s.writeMeta(metaPath, ns, now, ttl, n, contentType)
	if err != nil {
		_ = os.Remove(dataPath) // never leave data without its sidecar
		return err
	}
	if info.evictable {
		s.total += n + metaSize - prev
		if s.total > s.cfg.MaxBytes {
			s.evictLocked(now)
		}
	}
	return nil
}

// writeMeta writes the sidecar last, atomically, and returns its size.
func (s *store) writeMeta(path, ns string, now time.Time, ttl time.Duration, size int64, contentType string) (int64, error) {
	m := Meta{V: metaVersion, NS: ns, CreatedAt: now.UTC(), Size: size, ContentType: contentType}
	if ttl > 0 {
		exp := now.Add(ttl).UTC()
		m.ExpiresAt = &exp
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return 0, fmt.Errorf("cache: encode sidecar: %w", err)
	}
	tmp, _, err := writeTemp(filepath.Dir(path), bytes.NewReader(raw), int64(len(raw)), now)
	if err != nil {
		return 0, fmt.Errorf("cache: write sidecar: %w", err)
	}
	if err := os.Chmod(tmp, metaFileMode); err != nil {
		_ = os.Remove(tmp)
		return 0, fmt.Errorf("cache: sidecar mode: %w", err)
	}
	if err := replaceFile(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return 0, fmt.Errorf("cache: store sidecar: %w", err)
	}
	return int64(len(raw)), nil
}

// Delete removes one item.
func (s *store) Delete(ns, key string) error {
	info, err := namespaceInfo(ns)
	if err != nil {
		return err
	}
	if err := validKey(key); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	freed := s.itemBytes(ns, key, info)
	if err := s.removeFiles(ns, key, info); err != nil {
		return fmt.Errorf("cache: delete: %w", err)
	}
	if info.evictable {
		s.total = max(s.total-freed, 0)
	}
	return nil
}
