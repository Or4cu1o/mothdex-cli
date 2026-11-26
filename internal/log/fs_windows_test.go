//go:build windows

package log

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestAnotherUsersFolderOrFileIsRefused(t *testing.T) {
	dir := logDir(t)
	plant(t, dir, ".", LatestName)
	path := filepath.Join(dir, LatestName)
	if err := safeDir(dir); err != nil {
		t.Fatalf("the user's own folder: %v", err)
	}
	f, err := openFile(path, os.O_RDONLY)
	if err != nil {
		t.Fatalf("the user's own file: %v", err)
	}
	_ = f.Close()

	// the test may run elevated, with files owned by Administrators: refuse those too
	real, system := isOwn, systemOwners
	t.Cleanup(func() { isOwn, systemOwners = real, system })
	isOwn, systemOwners = func(*windows.SID) bool { return false }, nil
	if err := safeDir(dir); !errors.Is(err, errUnsafeDir) {
		t.Errorf("another user's folder: %v", err)
	}
	if _, err := openFile(path, os.O_RDONLY); !errors.Is(err, errUnsafeDir) {
		t.Errorf("another user's file: %v", err)
	}
	f, _, err = createNew(dir, "fresh", ".log")
	if err != nil {
		t.Fatalf("a file this process creates: %v", err)
	}
	_ = f.Close()
}

// A folder an elevated run created must stay usable; another user's must not.
func TestOwnersTheCheckAccepts(t *testing.T) {
	real := isOwn
	t.Cleanup(func() { isOwn = real })
	isOwn = func(*windows.SID) bool { return false }
	for owner, accepted := range map[string]bool{
		"S-1-5-32-544": true, // Administrators
		"S-1-5-18":     true, // SYSTEM
		"S-1-5-80-956008885-3418522649-1831038044-1853292631-2271478464": true, // TrustedInstaller
		"S-1-5-21-1111111111-2222222222-3333333333-1001":                 false,
		"S-1-5-32-545": false, // Users
		"S-1-1-0":      false, // Everyone
	} {
		sd, err := windows.SecurityDescriptorFromString("O:" + owner)
		if err != nil {
			t.Fatal(owner, err)
		}
		err = ownedByUser("folder", sd, nil)
		if accepted != (err == nil) || err != nil && !errors.Is(err, errUnsafeDir) {
			t.Errorf("owner %s: %v", owner, err)
		}
	}
}
