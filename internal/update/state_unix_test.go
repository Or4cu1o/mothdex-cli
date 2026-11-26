//go:build !windows

package update

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
)

func TestManagedUntrustedOwnerAndLinks(t *testing.T) {
	t.Run("group writable", func(t *testing.T) {
		u, s := fixture(t)
		_ = os.Chmod(s.Dir, 0770) //nolint:gosec // installation fixtures intentionally reproduce executable and state permissions.
		_, err := u.Managed()
		errorCode(t, err, app.CodeUpdateUnavailable, "untrusted_dir")
	})
	t.Run("foreign owner", func(t *testing.T) {
		if os.Geteuid() != 0 {
			t.Skip("requires root fixture ownership")
		}
		u, s := fixture(t)
		if err := os.Chown(s.Dir, 65534, 65534); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chown(s.Dir, 0, 0) })
		_, err := u.Managed()
		errorCode(t, err, app.CodeUpdateUnavailable, "not_owner")
	})
	t.Run("state symlink", func(t *testing.T) {
		u, s := fixture(t)
		p := filepath.Join(s.Dir, stateName)
		_ = os.Rename(p, p+".fixture")
		if err := os.Symlink(p+".fixture", p); err != nil {
			t.Fatal(err)
		}
		_, err := u.Managed()
		errorCode(t, err, app.CodeUpdateUnavailable, "bad_state")
	})
}
