package cache

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const lruTargetPercent = 90 // eviction stops at this share of the cap

// measure sums the bytes of the evictable namespaces with one scan.
func (s *store) measure() (int64, error) {
	var total int64
	now := s.clock.Now()
	for _, ns := range evictableNamespaces {
		items, err := s.scan(ns, now)
		if err != nil {
			return 0, err
		}
		for _, it := range items {
			total += it.bytes
		}
	}
	return total, nil
}

// evictLocked enforces the cap (caller holds s.mu): expired and corrupt items go first; if
// the total is still over the cap, least recently used items go until it is at most 90 % of
// the cap. Errors are logged: eviction is best effort.
func (s *store) evictLocked(now time.Time) {
	var live []itemInfo
	var total int64
	for _, ns := range evictableNamespaces {
		items, err := s.scan(ns, now)
		if err != nil {
			s.log.Debug("cache: scan failed during eviction", "ns", ns, "err", err)
			continue
		}
		for _, it := range items {
			if it.expired || it.corrupt {
				s.dropItem(it)
				continue
			}
			live = append(live, it)
			total += it.bytes
		}
	}
	if total > s.cfg.MaxBytes {
		total = s.evictLRU(live, total)
	}
	s.total = total
}

// evictLRU removes the oldest items (by data mtime) until total <= 90 % of the cap.
func (s *store) evictLRU(live []itemInfo, total int64) int64 {
	sort.Slice(live, func(i, j int) bool {
		a, b := live[i], live[j]
		if !a.mtime.Equal(b.mtime) {
			return a.mtime.Before(b.mtime)
		}
		return a.ns+"/"+a.key < b.ns+"/"+b.key
	})
	target := s.cfg.MaxBytes / 100 * lruTargetPercent
	for _, it := range live {
		if total <= target {
			break
		}
		if s.dropItem(it) {
			total -= it.bytes
		}
	}
	return total
}

// dropItem deletes an item's files and reports whether it succeeded.
func (s *store) dropItem(it itemInfo) bool {
	info, err := namespaceInfo(it.ns)
	if err == nil {
		err = s.removeFiles(it.ns, it.key, info)
	}
	if err != nil {
		s.log.Debug("cache: cannot delete item", "ns", it.ns, "err", err)
		return false
	}
	return true
}

// Prune removes expired items from every namespace and stale temp files, then enforces the
// cap on the evictable namespaces.
func (s *store) Prune() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clock.Now()
	var errs []error
	for _, ns := range allNamespaces {
		if err := s.purgeExpired(ns, now); err != nil {
			errs = append(errs, err)
		}
	}
	total, err := s.measure()
	if err != nil {
		return errors.Join(append(errs, err)...)
	}
	s.total = total
	if total > s.cfg.MaxBytes {
		s.evictLocked(now)
	}
	return errors.Join(errs...)
}

// purgeExpired removes expired and corrupt items and abandoned temp files of one namespace.
func (s *store) purgeExpired(ns string, now time.Time) error {
	items, err := s.scan(ns, now)
	if err != nil {
		return err
	}
	for _, it := range items {
		if it.expired || it.corrupt {
			s.dropItem(it)
		}
	}
	return s.removeStaleTemps(ns, now)
}

func (s *store) removeStaleTemps(ns string, now time.Time) error {
	entries, err := os.ReadDir(s.dir(ns))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), tmpPrefix) {
			continue
		}
		if st, err := e.Info(); err == nil && now.Sub(st.ModTime()) > staleTmpAge {
			if rerr := os.Remove(filepath.Join(s.dir(ns), e.Name())); rerr != nil {
				s.log.Debug("cache: cannot delete temp file", "err", rerr)
			}
		}
	}
	return nil
}

// Clear empties the given namespaces, or every evictable one when none is given.
func (s *store) Clear(ns ...string) error {
	if len(ns) == 0 {
		ns = evictableNamespaces
	}
	for _, n := range ns {
		if _, err := namespaceInfo(n); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var errs []error
	for _, n := range ns {
		if err := os.RemoveAll(s.dir(n)); err != nil {
			errs = append(errs, fmt.Errorf("cache: clear %s: %w", n, err))
		}
	}
	total, err := s.measure()
	if err != nil {
		errs = append(errs, err)
	}
	s.total = total
	return errors.Join(errs...)
}

// Stats reports items, bytes and expired items per namespace.
func (s *store) Stats() (Stats, error) {
	now := s.clock.Now()
	out := Stats{MaxBytes: s.cfg.MaxBytes}
	for _, ns := range allNamespaces {
		items, err := s.scan(ns, now)
		if err != nil {
			return Stats{}, err
		}
		row := NSStats{Name: ns}
		for _, it := range items {
			row.Items++
			row.Bytes += it.bytes
			if it.expired || it.corrupt {
				row.Expired++
			}
		}
		if namespaces[ns].evictable {
			out.TotalBytes += row.Bytes
		}
		out.Namespaces = append(out.Namespaces, row)
	}
	return out, nil
}
