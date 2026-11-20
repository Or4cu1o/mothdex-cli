package library

import (
	"fmt"
	"maps"
	"sort"
	"strings"
)

// Sources of a selected profile.
const (
	SourceAuto   = "auto"
	SourceConfig = "config"
	SourceFlag   = "flag"
)

// Config carries the user's choices; the app service fills it from config.md and flags.
type Config struct {
	Flag           string            // --profile
	ID             string            // profile.id ("" or "auto" = detect)
	ROMRoot        string            // profile.rom_root override
	RetroBatPath   string            // profile.retrobat_path
	Folders        map[string]string // profile.folders overrides
	RefreshOff     bool              // profile.refresh: false
	GenericROMRoot string            // generic ROM root: <paths.data>/ROMs, or <exe dir>/ROMs in portable mode
}

// ProfileInfo is the result of Detect. Profile is the resolved profile: variables expanded,
// folder overrides merged, refresh switched off when configured. Use Profile.DestDir and
// Profile.Refresh with it.
type ProfileInfo struct {
	ID, Name, Host string
	Source         string // auto | config | flag
	RomRoot        string
	Verified       bool     // false if any probe, path or hook used is unverified
	Unverified     []string // what to confirm (profile show prints them)
	Warnings       []string // why generic was chosen, ambiguity, unverified items
	Profile        Profile
}

// Detect selects a profile: --profile, then profile.id, then auto-detection by priority. It is
// pure (it only reads env) and never fails; any doubt selects generic with a warning.
func Detect(env Env, profiles []Profile, cfg Config) ProfileInfo {
	byID := make(map[string]Profile, len(profiles))
	for _, p := range profiles {
		byID[p.ID] = p
	}
	generic, ok := byID[GenericID]
	if !ok {
		generic = fallbackGeneric()
	}
	d := detector{env: env, cfg: cfg, generic: generic, vals: baseValues(env, cfg)}
	if id, source := forcedID(cfg); id != "" {
		return d.forced(byID, id, source)
	}
	return d.auto(profiles)
}

func baseValues(env Env, cfg Config) values {
	return values{home: env.Home, exeDir: env.ExeDir, config: map[string]string{"profile.retrobat_path": cfg.RetroBatPath}}
}

func forcedID(cfg Config) (id, source string) {
	switch {
	case cfg.Flag != "" && cfg.Flag != "auto":
		return cfg.Flag, SourceFlag
	case cfg.ID != "" && cfg.ID != "auto":
		return cfg.ID, SourceConfig
	default:
		return "", SourceAuto
	}
}

// fallbackGeneric is used only when no generic profile was loaded at all.
func fallbackGeneric() Profile {
	return Profile{Schema: SchemaVersion, ID: GenericID, Name: "Generic", OS: "any",
		Roms: Roms{Layout: LayoutPerPlatformID}, Launcher: Launcher{Kind: "none"}, Hook: Hook{Kind: RefreshNone}}
}

type detector struct {
	env     Env
	cfg     Config
	generic Profile
	vals    values
}

// forced honors the user's choice even when the probes fail; it only warns.
func (d detector) forced(byID map[string]Profile, id, source string) ProfileInfo {
	p, ok := byID[id]
	if !ok {
		info := d.build(d.generic, source, probeResult{}, []string{fmt.Sprintf("unknown profile %q: using generic", id)})
		return info
	}
	res := d.probe(p)
	var warns []string
	if !res.ok && p.ID != GenericID {
		warns = append(warns, fmt.Sprintf("profile %s was chosen explicitly but its detection probes do not match this system", p.ID))
	}
	info := d.build(p, source, res, warns)
	if p.ID != GenericID && !d.rootUsable(info.RomRoot) {
		info.Warnings = append(info.Warnings, fmt.Sprintf("ROM root %q is missing or not writable", info.RomRoot))
	}
	return info
}

type candidate struct {
	p   Profile
	res probeResult
}

func (d detector) auto(profiles []Profile) ProfileInfo {
	var found []candidate
	var warns []string
	for _, p := range profiles {
		if p.ID == GenericID || !osMatches(p.OS, d.env.GOOS) {
			continue
		}
		res := d.probe(p)
		if !res.ok {
			continue
		}
		if root := d.rootFor(p, res); !d.rootUsable(root) {
			warns = append(warns, fmt.Sprintf("profile %s detected but its ROM root %q is missing or not writable", p.ID, root))
			continue
		}
		found = append(found, candidate{p, res})
	}
	return d.choose(found, warns)
}

func (d detector) choose(found []candidate, warns []string) ProfileInfo {
	if len(found) == 0 {
		warns = append(warns, "no known system detected: using generic")
		return d.build(d.generic, SourceAuto, probeResult{}, warns)
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].p.Priority > found[j].p.Priority })
	top := found[0]
	if len(found) > 1 && found[1].p.Priority == top.p.Priority {
		ids := []string{top.p.ID}
		for _, c := range found[1:] {
			if c.p.Priority == top.p.Priority {
				ids = append(ids, c.p.ID)
			}
		}
		warns = append(warns, fmt.Sprintf("ambiguous system (%s match): using generic; set profile.id or --profile", strings.Join(ids, ", ")))
		return d.build(d.generic, SourceAuto, probeResult{}, warns)
	}
	return d.build(top.p, SourceAuto, top.res, warns)
}

func osMatches(profileOS, goos string) bool { return profileOS == "any" || profileOS == goos }

// probeResult is what the probes of one profile found.
type probeResult struct {
	ok      bool
	match   string  // value of ${MATCH}
	matched []Probe // probes that passed, for the unverified list
}

// probe runs the probes of p: every "all" probe, plus at least one "any" probe when present.
func (d detector) probe(p Profile) probeResult {
	res := probeResult{ok: true}
	record := func(pr Probe, m match) {
		res.matched = append(res.matched, pr)
		if res.match == "" {
			res.match = m.path
		}
	}
	for _, pr := range p.Detect.All {
		m := pr.run(d.env, d.vals)
		if !m.ok {
			return probeResult{}
		}
		record(pr, m)
	}
	if len(p.Detect.Any) == 0 {
		return res
	}
	for _, pr := range p.Detect.Any {
		if m := pr.run(d.env, d.vals); m.ok {
			record(pr, m)
			return res
		}
	}
	return probeResult{}
}

// rootFor resolves the ROM root: the user's override, the generic root, or the profile's
// expanded root. An unresolvable root is "".
func (d detector) rootFor(p Profile, res probeResult) string {
	root, _ := d.rootAndWarning(p, res)
	return root
}

// ownRoot is the profile's own expanded ROM root (generic: the root the app computed).
func (d detector) ownRoot(p Profile, res probeResult) (string, error) {
	if p.ID == GenericID {
		return d.cfg.GenericROMRoot, nil
	}
	root, err := d.withMatch(res).expand(p.Roms.Root)
	if err != nil {
		return "", err
	}
	return root, nil
}

// rootAndWarning is rootFor plus the reason an override was refused: a profile.rom_root that
// is, or lies inside, the launcher directory is ignored (T-16 case 15).
func (d detector) rootAndWarning(p Profile, res probeResult) (root, warn string) {
	v := d.withMatch(res)
	own, _ := d.ownRoot(p, res)
	if d.cfg.ROMRoot == "" {
		return own, ""
	}
	launcherDir, _ := v.expand(p.Launcher.Dir)
	probe := p
	probe.Launcher.Dir, probe.ownRoot = launcherDir, own
	if probe.IsLauncherPath(d.cfg.ROMRoot) {
		return own, fmt.Sprintf("profile.rom_root %q is a launcher folder: ignored", d.cfg.ROMRoot)
	}
	return d.cfg.ROMRoot, ""
}

func (d detector) withMatch(res probeResult) values {
	v := d.vals
	v.match = res.match
	return v
}

// rootUsable is true when the ROM root exists in the file tree and accepts files.
func (d detector) rootUsable(root string) bool {
	if root == "" || !isDir(d.env.FS, root) {
		return false
	}
	return d.env.Writable != nil && d.env.Writable(root)
}

// build resolves the chosen profile into a ProfileInfo.
func (d detector) build(p Profile, source string, res probeResult, warns []string) ProfileInfo {
	v := d.withMatch(res)
	resolved := p
	var rootWarn string
	resolved.Roms.Root, rootWarn = d.rootAndWarning(p, res)
	if rootWarn != "" {
		warns = append(warns, rootWarn)
	}
	resolved.ownRoot, _ = d.ownRoot(p, res)
	resolved.BinDir.Path, warns = expandOrWarn(v, p.BinDir.Path, "bin_dir", p.ID, warns)
	resolved.Launcher.Dir, warns = expandOrWarn(v, p.Launcher.Dir, "launcher.dir", p.ID, warns)
	resolved.Roms.Folders, warns = d.mergeFolders(p, resolved.Launcher.Dir, warns)
	if d.cfg.RefreshOff {
		resolved.Hook = Hook{Kind: RefreshNone}
	}
	if resolved.Roms.Root == "" && p.ID != GenericID {
		warns = append(warns, fmt.Sprintf("profile %s: ROM root could not be resolved; set profile.rom_root or use -o", p.ID))
	}
	info := ProfileInfo{
		ID: p.ID, Name: p.Name, Host: d.host(), Source: source, RomRoot: resolved.Roms.Root,
		Unverified: d.unverified(p, res), Warnings: warns, Profile: resolved,
	}
	info.Verified = len(info.Unverified) == 0
	if !info.Verified {
		info.Warnings = append(info.Warnings, fmt.Sprintf("%d unverified item(s); run profile show", len(info.Unverified)))
	}
	return info
}

// expandOrWarn expands a path; on failure the path is empty (never a partial one such as
// "/.local/bin" from a missing home) and a warning says why.
func expandOrWarn(v values, raw, field, id string, warns []string) (string, []string) {
	out, err := v.expand(raw)
	if err != nil {
		return "", append(warns, fmt.Sprintf("profile %s: %s could not be resolved (%v)", id, field, err))
	}
	return out, warns
}

// mergeFolders overlays the user's folder overrides on the profile map. An override that
// breaks CR-2/CR-3 is skipped with a warning.
func (d detector) mergeFolders(p Profile, launcherDir string, warns []string) (map[string]string, []string) {
	merged := maps.Clone(p.Roms.Folders)
	if merged == nil {
		merged = map[string]string{}
	}
	ids := make([]string, 0, len(d.cfg.Folders))
	for id := range d.cfg.Folders {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		folder := d.cfg.Folders[id]
		if !platformIDPattern.MatchString(id) {
			warns = append(warns, fmt.Sprintf("profile.folders.%s ignored: not a platform id", id))
		} else if err := checkFolder(folder, launcherNames(launcherDir)); err != nil {
			warns = append(warns, fmt.Sprintf("profile.folders.%s ignored: %v", id, err))
		} else {
			merged[id] = folder
		}
	}
	return merged, warns
}

// host reports the host system when it is not a ROM layout of its own (Bazzite).
func (d detector) host() string {
	if readOSRelease(d.env.FS)["ID"] == "bazzite" {
		return "bazzite"
	}
	return ""
}

// unverified lists what a beta tester should confirm for the chosen profile.
func (d detector) unverified(p Profile, res probeResult) []string {
	var out []string
	for _, pr := range res.matched {
		if !pr.Verified {
			out = append(out, "detection probe: "+pr.describe())
		}
	}
	if p.ID == GenericID {
		return out
	}
	if !p.Roms.RootVerified && d.cfg.ROMRoot == "" {
		out = append(out, "ROM root: "+p.Roms.Root)
	}
	if !p.Roms.FoldersVerified && len(p.Roms.Folders) > 0 {
		out = append(out, "folder names")
	}
	if p.BinDir.Path != "" && !p.BinDir.Verified {
		out = append(out, "bin dir: "+p.BinDir.Path)
	}
	if p.Launcher.Kind != "none" && p.Launcher.Kind != "" && !p.Launcher.Verified {
		out = append(out, "launcher: "+p.Launcher.Dir)
	}
	if (p.Hook.Kind == RefreshHTTPGet || p.Hook.Kind == RefreshManual) && !p.Hook.Verified && !d.cfg.RefreshOff {
		out = append(out, "refresh hook: "+p.Hook.Kind)
	}
	return out
}
