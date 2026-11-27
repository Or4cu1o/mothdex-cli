package update

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const stateName = ".mothdex-install.json"

// State is the v1 installer record. Paths are data and never direct update writes.
type State struct {
	extra         []stateField
	SchemaVersion int    `json:"schema_version"`
	Source        string `json:"source"`
	Installer     string `json:"installer"`
	Dir           string `json:"dir"`
	Version       string `json:"version"`
	Target        string `json:"target"`
	Profile       string `json:"profile"`
	GUI           bool   `json:"gui"`
	Alias         string `json:"alias"`
	AliasKind     string `json:"alias_kind"`
	AliasFileID   string `json:"alias_file_id"`
	PathAdded     bool   `json:"path_added"`
	PathKind      string `json:"path_kind"`
	PathFile      string `json:"path_file"`
	PathValue     string `json:"path_value"`
	Launcher      string `json:"launcher"`
	InstalledAt   string `json:"installed_at"`
	UpdatedAt     string `json:"updated_at"`
	UpdatedBy     string `json:"updated_by"`
}

// InstallCommand is the recovery command for this OS.
func InstallCommand() string { return installCommand(runtime.GOOS) }
func installCommand(goos string) string {
	if goos == "windows" {
		return "irm https://github.com/Or4cu1o/mothdex-cli/releases/latest/download/install.ps1 | iex"
	}
	return "curl -fsSL https://github.com/Or4cu1o/mothdex-cli/releases/latest/download/install.sh | sh"
}
func target() string {
	arch := runtime.GOARCH
	if arch == "arm" {
		arch = ""
		if bi, ok := debug.ReadBuildInfo(); ok {
			for _, s := range bi.Settings {
				if s.Key == "GOARM" {
					if strings.HasPrefix(s.Value, "6") {
						arch = "armv6"
					}
					if strings.HasPrefix(s.Value, "7") {
						arch = "armv7"
					}
				}
			}
		}
	}
	return runtime.GOOS + "_" + arch
}
func regular(path string) (os.FileInfo, error) {
	st, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() || isReparse(st) {
		return nil, fmt.Errorf("not a regular file")
	}
	return st, nil
}
func readBounded(path string, limit int64) ([]byte, error) {
	if _, err := regular(path); err != nil {
		return nil, err
	}
	f, err := os.Open(path) //nolint:gosec // caller supplies only fixed state/cache paths and regular() rejects links.
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("file too large")
	}
	return b, err
}
func decode(b []byte, v any, strict bool) error {
	if !utf8.Valid(b) {
		return fmt.Errorf("invalid UTF-8 JSON")
	}
	d := json.NewDecoder(bytes.NewReader(bytes.TrimPrefix(b, []byte{0xef, 0xbb, 0xbf})))
	if strict {
		d.DisallowUnknownFields()
	}
	if err := d.Decode(v); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("trailing JSON")
	}
	return nil
}

// Managed revalidates state and directory trust without writing to the installation.
func (u *Updater) Managed() (State, error) {
	var s State
	if !validVersion(u.o.Current) || strings.HasSuffix(target(), "_") {
		return s, unavailable("dev_build", nil)
	}
	dir := filepath.Dir(u.o.Exe)
	b, err := readBounded(filepath.Join(dir, stateName), 16<<10)
	if err != nil {
		reason := "bad_state"
		if os.IsNotExist(err) {
			reason = "no_state"
		}
		return s, unavailable(reason, err)
	}
	if err = decode(b, &s, false); err != nil {
		return s, unavailable("bad_state", err)
	}
	if s.SchemaVersion > 1 {
		return s, unavailable("state_newer", nil)
	}
	if s.SchemaVersion != 1 {
		return s, unavailable("bad_state", nil)
	}
	if s.Source != "installer" {
		return s, unavailable("not_installer", nil)
	}
	resolved, err := filepath.EvalSymlinks(s.Dir)
	if err != nil || !samePath(resolved, dir) {
		return s, unavailable("moved", err)
	}
	if reason, err := trustedDir(dir); err != nil {
		return s, unavailable(reason, err)
	}
	if !validState(s) {
		return s, unavailable("bad_state", nil)
	}
	return s, nil
}

func writeProbe(dir string) error {
	f, err := os.CreateTemp(dir, ".mothdex-write-probe-")
	if err != nil {
		return unavailable("read_only", err)
	}
	name := f.Name()
	cerr := f.Close()
	rerr := os.Remove(name)
	if cerr != nil || rerr != nil {
		return unavailable("read_only", fmt.Errorf("write probe cleanup failed"))
	}
	return nil
}
func samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

var aliasPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,15}$`)
var profilePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

func validAlias(s string) bool {
	if !aliasPattern.MatchString(s) {
		return false
	}
	switch s {
	case "mothdex", "mothdex-gui", "con", "prn", "aux", "nul":
		return false
	}
	if len(s) == 4 && (strings.HasPrefix(s, "com") || strings.HasPrefix(s, "lpt")) && s[3] >= '0' && s[3] <= '9' {
		return false
	}
	return true
}
func validState(s State) bool {
	if !validVersion(s.Version) || s.Target != target() || !filepath.IsAbs(s.Dir) || !profilePattern.MatchString(s.Profile) {
		return false
	}
	if s.Installer != "install.sh" && s.Installer != "install.ps1" {
		return false
	}
	if s.UpdatedBy != "installer" && s.UpdatedBy != "upgrade" {
		return false
	}
	for _, v := range []string{s.InstalledAt, s.UpdatedAt} {
		t, e := time.Parse(time.RFC3339, v)
		if e != nil || t.Location() != time.UTC {
			return false
		}
	}
	if s.Launcher != "" && !filepath.IsAbs(s.Launcher) {
		return false
	}
	if s.Alias == "" {
		if s.AliasKind != "" || s.AliasFileID != "" {
			return false
		}
	} else {
		if !validAlias(s.Alias) {
			return false
		}
		kind := "symlink"
		if runtime.GOOS == "windows" {
			kind = "hardlink"
		}
		if s.AliasKind != kind || (kind != "hardlink" && s.AliasFileID != "") || (s.AliasFileID != "" && !validFileID(s.AliasFileID)) {
			return false
		}
	}
	if !s.PathAdded {
		return s.PathKind == "" && s.PathFile == "" && s.PathValue == ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	switch s.PathKind {
	case "user_env":
		return runtime.GOOS == "windows" && s.PathFile == "" && samePath(s.PathValue, s.Dir)
	case "rc_line":
		allowed := false
		for _, n := range []string{".bashrc", ".bash_profile", ".zshrc", ".profile"} {
			allowed = allowed || samePath(s.PathFile, filepath.Join(home, n))
		}
		return runtime.GOOS != "windows" && allowed && s.PathValue == `export PATH="`+s.Dir+`:$PATH" # mothdex`
	case "fish":
		return runtime.GOOS != "windows" && samePath(s.PathFile, filepath.Join(home, ".config", "fish", "config.fish")) && s.PathValue == s.Dir
	}
	return false
}
func atomicJSON(path string, v any, mode os.FileMode) error {
	if st, err := os.Lstat(path); err == nil && (!st.Mode().IsRegular() || isReparse(st)) {
		return fmt.Errorf("unsafe destination")
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	var b []byte
	var err error
	if s, ok := v.(State); ok {
		b, err = marshalState(s)
	} else {
		b, err = json.MarshalIndent(v, "", "  ")
	}
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".mothdex-json-")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(append(b, '\n'))
	}
	if err == nil {
		err = f.Sync()
	}
	ce := f.Close()
	if err == nil {
		err = ce
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func validFileID(id string) bool {
	parts := strings.Split(id, ":")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if len(part) < 1 || len(part) > 10 || !numeric(part) {
			return false
		}
		if _, err := strconv.ParseUint(part, 10, 32); err != nil {
			return false
		}
	}
	return true
}
