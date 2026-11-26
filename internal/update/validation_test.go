package update

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestStateRejectsInvalidUTF8(t *testing.T) {
	u, s := fixture(t)
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	b = append(b[:len(b)-1], []byte(`,"unknown":"`)...)
	b = append(b, 0xff)
	b = append(b, []byte(`"}`)...)
	if err = os.WriteFile(filepath.Join(s.Dir, stateName), b, 0600); err != nil {
		t.Fatal(err)
	}
	_, err = u.Managed()
	errorCode(t, err, "update_unavailable", "bad_state")
}
func TestExtractionRejectsConcatenatedArchives(t *testing.T) {
	if filepath.Ext(archiveName("2.0.0", target(), false)) == ".zip" {
		t.Skip("ZIP parser handles its own central directory")
	}
	one := archiveFixture(t, []byte("fixture"), "")
	body := append(append([]byte{}, one...), one...)
	_, err := extract(body, t.TempDir(), false)
	errorCode(t, err, "integrity", "archive_trailing_data")
}
func TestExtractionWriteFailureUsesContractReason(t *testing.T) {
	_, err := extract(archiveFixture(t, []byte("fixture"), ""), filepath.Join(t.TempDir(), "absent"), false)
	errorCode(t, err, "update_failed", "swap_failed")
}
