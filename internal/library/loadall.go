package library

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
)

// LoadResult is the outcome of LoadAll. A rejected profile is not loaded: it is reported in
// Errors and detection carries on without it.
type LoadResult struct {
	Profiles []Profile // sorted by priority (high first), then id
	Warnings []string
	Errors   []error
}

// LoadAll loads the embedded profiles and then the user's overrides. A user file replaces the
// embedded profile with the same id, or adds a new one. user may be nil.
func LoadAll(embedded, user fs.FS, o LoadOptions) LoadResult {
	var res LoadResult
	byID := map[string]Profile{}
	for _, src := range []fs.FS{embedded, user} {
		if src == nil {
			continue
		}
		loadDir(src, o, byID, &res)
	}
	for _, p := range byID {
		res.Profiles = append(res.Profiles, p)
	}
	sort.Slice(res.Profiles, func(i, j int) bool {
		a, b := res.Profiles[i], res.Profiles[j]
		if a.Priority != b.Priority {
			return a.Priority > b.Priority
		}
		return a.ID < b.ID
	})
	return res
}

func loadDir(src fs.FS, o LoadOptions, byID map[string]Profile, res *LoadResult) {
	entries, err := fs.ReadDir(src, ".")
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			res.Errors = append(res.Errors, fmt.Errorf("read profiles: %w", err))
		}
		return
	}
	for _, e := range entries {
		if e.IsDir() || path.Ext(e.Name()) != ".yaml" {
			continue
		}
		raw, err := fs.ReadFile(src, e.Name())
		if err != nil {
			res.Errors = append(res.Errors, fmt.Errorf("read %s: %w", e.Name(), err))
			continue
		}
		p, warns, err := Load(raw, o)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Errorf("%s: %w", e.Name(), err))
			continue
		}
		if want := strings.TrimSuffix(e.Name(), ".yaml"); want != p.ID {
			res.Errors = append(res.Errors, fmt.Errorf("%s: %w: file name must match id %q", e.Name(), ErrInvalidProfile, p.ID))
			continue
		}
		res.Warnings = append(res.Warnings, warns...)
		byID[p.ID] = p
	}
}
