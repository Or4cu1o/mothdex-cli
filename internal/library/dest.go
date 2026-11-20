package library

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// Errors of DestDir.
var (
	ErrUnmappedPlatform = errors.New("platform has no folder on this system")
	ErrInvalidPlatform  = errors.New("invalid platform id")
	ErrNoRomRoot        = errors.New("no ROM root for this profile")
)

// DestDir returns the destination directory for a platform: RomRoot/<folder>. In every
// layout the platform id must match ^[a-z0-9][a-z0-9_-]{0,31}$ and the folder is a single
// validated component, so the result always stays inside RomRoot. Membership of the id in
// /platforms is the caller's check (it holds the platform list). The profile must be the
// resolved one from ProfileInfo, whose Roms.Root is already expanded.
func (p Profile) DestDir(platformID string) (string, error) {
	if !platformIDPattern.MatchString(platformID) {
		return "", fmt.Errorf("%w: %q", ErrInvalidPlatform, platformID)
	}
	if p.Roms.Root == "" {
		return "", ErrNoRomRoot
	}
	folder, err := p.folderFor(platformID)
	if err != nil {
		return "", err
	}
	if err := checkFolder(folder, launcherNames(p.Launcher.Dir)); err != nil {
		return "", fmt.Errorf("folder for %q: %w", platformID, err)
	}
	return joinPath(p.OS, p.Roms.Root, folder), nil
}

func (p Profile) folderFor(platformID string) (string, error) {
	if p.Roms.Layout == LayoutPerPlatformID {
		return platformID, nil
	}
	if folder, ok := p.Roms.Folders[platformID]; ok {
		return folder, nil
	}
	if p.Roms.Unmapped == UnmappedPlatformID {
		return platformID, nil
	}
	return "", fmt.Errorf("%w: %q (use -o <dir> or set profile.folders.%s)", ErrUnmappedPlatform, platformID, platformID)
}

// joinPath joins with the separator of the profile's OS, so a Windows profile gives the same
// answer on any host. Profiles for "any" OS use the host separator.
func joinPath(osName, root, elem string) string {
	sep := "/"
	switch osName {
	case "windows":
		sep = `\`
	case "any":
		return filepath.Join(root, elem)
	}
	return strings.TrimRight(root, `/\`) + sep + elem
}
