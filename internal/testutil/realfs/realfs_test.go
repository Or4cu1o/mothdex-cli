package realfs_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Or4cu1o/mothdex-cli/internal/testutil/realfs"
)

func TestSnapshotDetectsChangesAndNeverFollowsLinks(t *testing.T) {
	root, outside := realfs.Dir(t), realfs.Dir(t)
	realfs.WriteFile(t, filepath.Join(root, "a", "f.bin"), []byte("one"))
	realfs.WriteFile(t, filepath.Join(outside, "victim"), []byte("v"))
	realfs.DirLink(t, outside, filepath.Join(root, "link"))

	before := realfs.Snapshot(t, root)
	if err := os.WriteFile(filepath.Join(outside, "victim"), []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if diff := realfs.DiffSnapshots(before, realfs.Snapshot(t, root)); len(diff) != 0 {
		t.Fatalf("a change behind the link leaked into the snapshot: %v", diff)
	}

	if err := os.WriteFile(filepath.Join(root, "a", "f.bin"), []byte("two"), 0o600); err != nil {
		t.Fatal(err)
	}
	realfs.WriteFile(t, filepath.Join(root, "new.bin"), []byte("n"))
	diff := realfs.DiffSnapshots(before, realfs.Snapshot(t, root))
	if len(diff) != 2 {
		t.Fatalf("diff = %v, want one changed and one added", diff)
	}
}

func TestIsCaseInsensitiveLeavesNoProbe(t *testing.T) {
	dir := realfs.Dir(t)
	_ = realfs.IsCaseInsensitive(t, dir)
	if names := realfs.Names(t, dir); len(names) != 0 {
		t.Fatalf("probe left %v behind", names)
	}
}
