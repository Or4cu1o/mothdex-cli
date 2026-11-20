// Package library evaluates system profiles (docs/contracts/cli/profiles.md): it decides
// where ROMs go on a given system and how to make the frontend notice new files. Profiles
// are data (YAML embedded in the binary, overridable by the user); this package only
// evaluates them. Detection is a pure function over an injected environment and never fails
// a command: any doubt selects the generic profile with a warning.
package library

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/Or4cu1o/mothdex-cli/internal/config"
)

// SchemaVersion is the profile schema this build understands.
const SchemaVersion = 1

// Layouts, unmapped policies, OS values and kinds of the schema.
const (
	LayoutPerSystem     = "per_system"
	LayoutPerPlatformID = "per_platform_id"
	UnmappedError       = "error"
	UnmappedPlatformID  = "platform_id"
	RefreshHTTPGet      = "http_get"
	RefreshManual       = "manual"
	RefreshNone         = "none"
	GenericID           = "generic"
)

// Probe is one read-only detection test. Exactly one of the kind fields is set.
type Probe struct {
	File      string            `yaml:"file"`
	Files     []string          `yaml:"files"` // every file must exist
	Dir       string            `yaml:"dir"`
	Glob      string            `yaml:"glob"`       // at least one match, at most two wildcard levels
	OSRelease map[string]string `yaml:"os_release"` // exact, case-sensitive KEY=value pairs
	Config    string            `yaml:"config"`     // config key whose value is non-empty and exists
	Verified  bool              `yaml:"verified"`
}

// Probes lists the detection probes of a profile: all of All, plus at least one of Any when present.
type Probes struct {
	All []Probe `yaml:"all"`
	Any []Probe `yaml:"any"`
}

// Roms describes the ROM destination.
type Roms struct {
	Root            string            `yaml:"root"`
	RootVerified    bool              `yaml:"root_verified"`
	FoldersVerified bool              `yaml:"folders_verified"`
	Layout          string            `yaml:"layout"`
	Unmapped        string            `yaml:"unmapped"`
	Folders         map[string]string `yaml:"folders"` // Mothdex platform id -> folder
}

// BinDir is the persistent directory for the installer.
type BinDir struct {
	Path     string `yaml:"path"`
	Verified bool   `yaml:"verified"`
}

// Launcher is the port or launcher the installer creates.
type Launcher struct {
	Kind     string `yaml:"kind"` // sh | bat | none
	Dir      string `yaml:"dir"`
	Verified bool   `yaml:"verified"`
}

// Hook is the refresh hook that makes the frontend notice new files after a batch.
type Hook struct {
	Kind     string        `yaml:"kind"` // http_get | manual | none
	URL      string        `yaml:"url"`
	Timeout  time.Duration `yaml:"timeout"`
	Verified bool          `yaml:"verified"`
}

// Profile is one system profile.
type Profile struct {
	Schema   int      `yaml:"schema"`
	ID       string   `yaml:"id"`
	Name     string   `yaml:"name"`
	OS       string   `yaml:"os"` // linux | windows | darwin | any
	Priority int      `yaml:"priority"`
	Detect   Probes   `yaml:"detect"`
	Roms     Roms     `yaml:"roms"`
	BinDir   BinDir   `yaml:"bin_dir"`
	Launcher Launcher `yaml:"launcher"`
	Hook     Hook     `yaml:"refresh"`

	ownRoot string // the profile's own expanded ROM root, set by Detect (not data)
}

// LoadOptions tunes loading.
type LoadOptions struct {
	// Platforms is the set of Mothdex platform ids (from /platforms). When non-nil, folder
	// keys outside it become load warnings.
	Platforms map[string]bool
}

var (
	profileIDPattern  = regexp.MustCompile(`^[a-z0-9-]+$`)
	platformIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)
)

// ErrInvalidProfile wraps every rejection of a profile by the loader.
var ErrInvalidProfile = errors.New("invalid profile")

// Load parses and validates one profile. A rejected profile is an error (it is not loaded);
// soft problems come back as warnings.
func Load(data []byte, o LoadOptions) (Profile, []string, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var p Profile
	if err := dec.Decode(&p); err != nil {
		return Profile{}, nil, fmt.Errorf("%w: %w", ErrInvalidProfile, err)
	}
	warnings, err := p.validate(o)
	if err != nil {
		return Profile{}, nil, fmt.Errorf("%w: %s: %w", ErrInvalidProfile, p.ID, err)
	}
	return p, warnings, nil
}

func (p Profile) validate(o LoadOptions) ([]string, error) {
	if p.Schema != SchemaVersion {
		return nil, fmt.Errorf("schema must be %d", SchemaVersion)
	}
	if !profileIDPattern.MatchString(p.ID) {
		return nil, errors.New("id must match [a-z0-9-]+")
	}
	if !oneOf(p.OS, "linux", "windows", "darwin", "any") {
		return nil, errors.New("os must be linux, windows, darwin or any")
	}
	if err := p.validateProbes(); err != nil {
		return nil, err
	}
	if err := p.validatePaths(); err != nil {
		return nil, err
	}
	if err := p.validateRefresh(); err != nil {
		return nil, err
	}
	return p.validateFolders(o)
}

func oneOf(v string, allowed ...string) bool {
	for _, a := range allowed {
		if v == a {
			return true
		}
	}
	return false
}

func (p Profile) validatePaths() error {
	if !oneOf(p.Roms.Layout, LayoutPerSystem, LayoutPerPlatformID) {
		return errors.New("roms.layout must be per_system or per_platform_id")
	}
	if !oneOf(p.Roms.Unmapped, UnmappedError, UnmappedPlatformID, "") {
		return errors.New("roms.unmapped must be error or platform_id")
	}
	if p.Roms.Root == "" && p.ID != GenericID {
		return errors.New("roms.root is required")
	}
	if !oneOf(p.Launcher.Kind, "sh", "bat", "none", "") {
		return errors.New("launcher.kind must be sh, bat or none")
	}
	for _, path := range []string{p.Roms.Root, p.BinDir.Path, p.Launcher.Dir} {
		if err := checkVariables(path); err != nil {
			return err
		}
		if err := checkAbsolute(path); err != nil {
			return err
		}
	}
	if err := p.checkMatchUse(); err != nil {
		return err
	}
	if p.Launcher.Kind != "none" && p.Launcher.Kind != "" && p.Launcher.Dir == "" {
		return errors.New("launcher.dir is required for sh and bat launchers")
	}
	return nil
}

func (p Profile) validateRefresh() error {
	switch p.Hook.Kind {
	case RefreshHTTPGet:
		if err := ValidateRefreshURL(p.Hook.URL); err != nil {
			return err
		}
		if p.Hook.Timeout < 0 || p.Hook.Timeout > RefreshTimeout {
			return fmt.Errorf("refresh.timeout must be at most %s", RefreshTimeout)
		}
	case RefreshManual, RefreshNone, "":
		if p.Hook.URL != "" {
			return errors.New("refresh.url is only valid for http_get")
		}
	default:
		return errors.New("refresh.kind must be http_get, manual or none")
	}
	return nil
}

// validateFolders applies CR-2/CR-3: keys are platform ids, values are single local
// components that are not launcher folders.
func (p Profile) validateFolders(o LoadOptions) ([]string, error) {
	var warnings []string
	launcher := launcherNames(p.Launcher.Dir)
	for id, folder := range p.Roms.Folders {
		if !platformIDPattern.MatchString(id) {
			return nil, fmt.Errorf("folder key %q is not a platform id", id)
		}
		if err := checkFolder(folder, launcher); err != nil {
			return nil, fmt.Errorf("folder %q: %w", id, err)
		}
		if o.Platforms != nil && !o.Platforms[id] {
			warnings = append(warnings, fmt.Sprintf("profile %s: folder key %q is not a Mothdex platform id", p.ID, id))
		}
	}
	return warnings, nil
}

// launcherNames returns the folder names that must never receive downloads: the fixed launcher
// folders plus the last component of the profile's launcher directory.
func launcherNames(launcherDir string) []string {
	names := config.LauncherFolders()
	if last := lastComponent(launcherDir); last != "" {
		names = append(names, last)
	}
	return names
}

func lastComponent(path string) string {
	path = strings.TrimRight(path, `/\`)
	if i := strings.LastIndexAny(path, `/\`); i >= 0 {
		return path[i+1:]
	}
	return path
}

// checkAbsolute requires a path to be absolute (or to start with a variable, which expands to
// an absolute path). An empty path is allowed where the field is optional.
func checkAbsolute(p string) error {
	if p == "" || strings.HasPrefix(p, "/") || strings.HasPrefix(p, "${") || driveRooted.MatchString(p) {
		return nil
	}
	return fmt.Errorf("path %q must be absolute", p)
}

var driveRooted = regexp.MustCompile(`^[A-Za-z]:[\\/]`)

// IsLauncherPath reports whether path is, or lies inside, a place where the frontend runs
// scripts: the profile's launcher directory, or a folder named ports, ports_scripts or windows
// directly under the profile's own default ROM root. Paths are normalized first (. and .. are
// resolved, the \\?\ prefix is dropped, trailing dots and spaces are cut as Windows does), so
// equivalent spellings give the same answer. Comparison is per component (never a string prefix) and ignores
// case and look-alike letters (portſ). The preflight applies it to -o and profile.rom_root,
// and DestDir to every result (T-16 case 15, downloader 6.2 G3). Use it on the resolved
// profile from ProfileInfo.
func (p Profile) IsLauncherPath(path string) bool {
	target := components(path)
	if len(target) == 0 {
		return false
	}
	if dir := components(p.Launcher.Dir); len(dir) > 0 && within(target, dir) {
		return true
	}
	if root := components(p.ownRoot); len(root) > 0 {
		for _, l := range config.LauncherFolders() {
			if within(target, append(append([]string{}, root...), strings.ToUpper(l))) {
				return true
			}
		}
	}
	return false
}

// components normalizes a path written with either separator into upper-cased components,
// so that comparison ignores case, folds look-alikes the way config.ValidateFolderName does,
// and sees through spellings the OS treats as the same folder: "." and ".." components, the
// Windows \\?\ and \\.\ prefixes, and trailing dots or spaces on a component.
func components(p string) []string {
	for _, prefix := range []string{`\\?\`, `\\.\`, `//?/`, `//./`} {
		p = strings.TrimPrefix(p, prefix)
	}
	var out []string
	for _, c := range strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == '\\' }) {
		switch c {
		case ".":
			continue
		case "..":
			if len(out) > 0 {
				out = out[:len(out)-1]
			}
			continue
		}
		if c = strings.TrimRight(c, ". "); c != "" {
			out = append(out, strings.ToUpper(c))
		}
	}
	return out
}

// within reports whether target equals dir or lies inside it, component by component.
func within(target, dir []string) bool {
	if len(target) < len(dir) {
		return false
	}
	for i := range dir {
		if target[i] != dir[i] {
			return false
		}
	}
	return true
}

// checkMatchUse rejects ${MATCH} in a path when no probe of the profile can yield one (for
// example only os_release), which would otherwise fall back to generic silently at runtime.
func (p Profile) checkMatchUse() error {
	usesMatch := false
	for _, path := range []string{p.Roms.Root, p.BinDir.Path, p.Launcher.Dir} {
		usesMatch = usesMatch || strings.Contains(path, "${MATCH}")
	}
	if !usesMatch {
		return nil
	}
	for _, pr := range append(append([]Probe{}, p.Detect.All...), p.Detect.Any...) {
		if pr.yieldsPath() {
			return nil
		}
	}
	return errors.New("${MATCH} is used but no probe can yield a path")
}
