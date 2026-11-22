//go:build unix

package i18n

import (
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"testing/fstest"
	"time"
)

// A FIFO in the locale folder is refused from its stat: opening it would block until a writer appears and
// freeze every command at start-up (LoadDir runs on all of them).
func TestLoadDirDoesNotBlockOnAFifo(t *testing.T) {
	dir := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(dir, "xx_yy.yaml"), 0o600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	done := make(chan *Bundle, 1)
	go func() {
		b, _ := LoadDir(fstest.MapFS{"en_us.yaml": {Data: []byte("a: 'A'\n")}}, dir)
		done <- b
	}()
	select {
	case b := <-done:
		if ws := b.Warnings(); len(ws) != 1 || !strings.Contains(ws[0], "not a regular file") {
			t.Fatalf("warnings: %q", ws)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("LoadDir blocked on a FIFO")
	}
}
