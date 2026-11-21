package proxy

import (
	"fmt"
	"os"
)

// OpenStagingFS opens paths.temp as the staging file system and prepares its downloads/
// directory (0700). On POSIX systems it refuses a paths.temp that other users can write to
// unless the sticky bit is set (like /tmp), and a downloads/ directory that is not private
// and owned by the current user (TH-11): a predictable shared directory is how staging files
// get swapped. Windows relies on the ACLs of the user's own temp directory (not verified).
func OpenStagingFS(temp string) (*RootFS, error) {
	if err := os.MkdirAll(temp, 0o700); err != nil {
		return nil, fmt.Errorf("proxy: create the temp directory: %w", err)
	}
	if err := checkTempDir(temp); err != nil {
		return nil, err
	}
	r, err := OpenRootFS(temp)
	if err != nil {
		return nil, err
	}
	if err := r.MkdirAll(stagingDir, 0o700); err != nil {
		_ = r.Close()
		return nil, fmt.Errorf("proxy: create the staging directory: %w", err)
	}
	if err := checkPrivateDir(r.Dir() + string(os.PathSeparator) + stagingDir); err != nil {
		_ = r.Close()
		return nil, err
	}
	return r, nil
}
