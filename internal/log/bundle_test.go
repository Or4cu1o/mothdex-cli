package log

import (
	"archive/zip"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Or4cu1o/mothdex-cli/internal/config"
)

func unzip(t *testing.T, path string) map[string]string {
	t.Helper()
	z, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = z.Close() }()
	out := map[string]string{}
	for _, f := range z.File {
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(r)
		if err != nil {
			t.Fatal(err)
		}
		_ = r.Close()
		out[f.Name] = string(b)
	}
	return out
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := safeDir(filepath.Dir(path)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestBundleHidesThePersonAndTheAPI(t *testing.T) {
	dir := logDir(t)
	// what a v0.1.x log looks like: nothing was redacted
	write(t, filepath.Join(dir, FileName), `time=x level=ERROR msg=x err="Get \"https://api.mothdex.net/v1/info?k=S3CRET\": dial tcp: lookup api.mothdex.net"`+"\n"+
		`path=C:/Users/Bob/roms other="C:\\Users\\bob\\roms" long=\\?\C:\USERS\BOB\x usb=/run/media/bob/disk`+"\n")
	write(t, filepath.Join(dir, SessionsDir, "20260101-000000-cli-1.log"), "path=c:\\users\\bob\\file\nAuthorization: Bearer S3CRET\n")
	write(t, filepath.Join(dir, SessionsDir, "notes.log"), "S3CRET")
	write(t, filepath.Join(dir, CrashDir, "20260101-000000-cli-1.txt"), "panic at C:\\Users\\Bob\\go\\x.go\n")
	outside := filepath.Join(t.TempDir(), "outside")
	write(t, outside, "S3CRET from a link\n")
	_ = os.Symlink(outside, filepath.Join(dir, SessionsDir, "20260102-000000-cli-2.log"))

	res, err := Bundle(context.Background(), BundleOptions{
		Dir: dir, Home: `C:\Users\Bob`, Environ: []string{"MOTHDEX_API_BASE_URL=https://u:S3CRET@h/v1", "OTHER=S3CRET"},
		Manifest: Manifest{Version: "0.2.0", Config: []ConfigEntry{
			{Key: "api.base_url", Value: "https://my.private.api/v1", Source: "env"},
			{Key: "paths.data", Value: `D:\bob-data`, Source: "file"},
			{Key: "a.key.added.later", Value: "S3CRET", Source: "file"},
			{Key: "interface.language", Value: "pt_br", Source: "file"},
		}},
		Now: func() time.Time { return testStart },
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Path != filepath.Join(dir, BundlesDir, "mothdex-logs-20261123-014700.zip") || !slices.Equal(res.Sessions, []string{"20260101-000000-cli-1"}) {
		t.Errorf("result %+v", res)
	}
	files := unzip(t, res.Path)
	names := make([]string, 0, len(files))
	for name, content := range files {
		names = append(names, name)
		for _, secret := range []string{"S3CRET", "api.mothdex.net", "my.private.api", "bob-data"} {
			if strings.Contains(content, secret) {
				t.Errorf("%s holds %q:\n%s", name, secret, content)
			}
		}
		if strings.Contains(strings.ToLower(content), `users\bob`) || strings.Contains(strings.ToLower(content), "/bob/") ||
			strings.Contains(strings.ToLower(content), `users\\bob`) || strings.Contains(strings.ToLower(content), "users/bob") {
			t.Errorf("%s names the user:\n%s", name, content)
		}
	}
	slices.Sort(names)
	want := []string{"README.txt", "crash/20260101-000000-cli-1.txt", "manifest.json", "mothdex.log.tail", "sessions/20260101-000000-cli-1.log"}
	if !slices.Equal(names, want) {
		t.Errorf("entries %v", names)
	}
	manifest := files["manifest.json"]
	for _, want := range []string{`"MOTHDEX_API_BASE_URL"`, `"pt_br"`, `"<redacted>"`, `"schema_version": 1`} {
		if !strings.Contains(manifest, want) {
			t.Errorf("manifest lacks %s:\n%s", want, manifest)
		}
	}

	again, err := Bundle(context.Background(), BundleOptions{Dir: dir, Now: func() time.Time { return testStart }})
	if err != nil || again.Path == res.Path || !exists(res.Path) {
		t.Errorf("a second bundle in the same second: %+v, %v", again, err)
	}
}

// noise is about n bytes of text that does not compress much.
func noise(t *testing.T, n int) string {
	t.Helper()
	raw := make([]byte, n*3/4)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	line := base64.StdEncoding.EncodeToString(raw) // no "://" or "@" for the redactor to find
	var out strings.Builder
	for ; len(line) > 100; line = line[100:] {
		out.WriteString(line[:100] + "\n")
	}
	return out.String() + line + "\n"
}

func TestBundleStaysUnderTheLimit(t *testing.T) {
	const limit = 60 << 10
	dir := logDir(t)
	for _, d := range []string{"01", "02", "03", "04", "05", "06", "07"} {
		write(t, filepath.Join(dir, SessionsDir, "202601"+d+"-000000-cli-1.log"), noise(t, 20<<10))
	}
	write(t, filepath.Join(dir, FileName), "tail line\n")
	res, err := Bundle(context.Background(), BundleOptions{Dir: dir, Sessions: 7, limit: limit})
	if err != nil {
		t.Fatal(err)
	}
	if res.Bytes > limit || len(res.Sessions) < 2 || len(res.Sessions) >= 7 || res.Sessions[0] != "20260107-000000-cli-1" {
		t.Fatalf("%d bytes, sessions %v", res.Bytes, res.Sessions)
	}
	if unzip(t, res.Path)["mothdex.log.tail"] == "" {
		t.Error("the tail went before the old sessions")
	}

	for _, d := range []string{"01", "02", "03", "04", "05"} {
		write(t, filepath.Join(dir, CrashDir, "202601"+d+"-000000-cli-1.txt"), noise(t, 30<<10))
	}
	res, err = Bundle(context.Background(), BundleOptions{Dir: dir, Sessions: 7, limit: limit})
	if err != nil {
		t.Fatal(err)
	}
	files := unzip(t, res.Path)
	if res.Bytes > limit || len(res.Sessions) != 1 || files["mothdex.log.tail"] == "" || files["crash/20260105-000000-cli-1.txt"] == "" {
		t.Errorf("crash files must go before the newest session and the tail: %d bytes, %v", res.Bytes, res.Sessions)
	}
}

func TestBundleRefusesBadOptions(t *testing.T) {
	dir := logDir(t)
	file := filepath.Join(t.TempDir(), "file")
	write(t, file, "x")
	for _, c := range []struct {
		o    BundleOptions
		want error
	}{
		{BundleOptions{Dir: dir, Sessions: 21}, ErrSessions},
		{BundleOptions{Dir: dir, Sessions: -1}, ErrSessions},
		{BundleOptions{Dir: dir, OutDir: filepath.Join(dir, "missing")}, ErrOutDir},
		{BundleOptions{Dir: dir, OutDir: file}, ErrOutDir},
	} {
		if _, err := Bundle(context.Background(), c.o); !errors.Is(err, c.want) {
			t.Errorf("%+v => %v, want %v", c.o, err, c.want)
		}
	}
}

// Logs of an earlier version were never redacted: the bundle catches the ways a secret was written.
func TestBundleRedactsSecretsHoweverTheyWereWritten(t *testing.T) {
	dir := logDir(t)
	// %s is the secret, put in at run time so that no scanner reads these as real credentials
	forms := []string{
		`Authorization="Bearer %s"`, `err="Authorization=\"Bearer %s\""`, `Authorization: Bearer %s`,
		`{"authorization":"Bearer %s"}`, `Cookie="id=%s; s=2"`, `X-Api-Key: "%s"`,
		`{"token":"%s"}`, `msg="{\"access_token\":\"%s\"}"`, `{"password": "two words %s"}`,
		`api_key: %s`, `api-key=%s`, `secret='%s'`, `client_secret="%s"`, `sig=%s&x=1`,
	}
	text := strings.ReplaceAll(strings.Join(forms, "\n"), "%s", "S3CRET")
	write(t, filepath.Join(dir, FileName), text+"\nkept=value\n")
	res, err := Bundle(context.Background(), BundleOptions{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	tail := unzip(t, res.Path)["mothdex.log.tail"]
	if strings.Contains(tail, "S3CRET") || !strings.Contains(tail, "kept=value") {
		t.Errorf("tail:\n%s", tail)
	}
	if again := newRedactor().text(tail); again != tail {
		t.Errorf("a second pass changed the text:\n%s", again)
	}
}

// A setting added to the schema must be classified before it can appear in a bundle.
func TestEverySettingIsClassifiedForTheManifest(t *testing.T) {
	masked := []string{"api.base_url", "download.dir", "download.allowed_hosts", "paths.temp", "paths.data", "paths.logs",
		"profile.rom_root", "profile.retrobat_path"}
	for _, s := range config.Specs() {
		if slices.Contains(clearKeys, s.Key) == slices.Contains(masked, s.Key) {
			t.Errorf("classify %s: add it to clearKeys (bundle.go) or to the masked list of this test", s.Key)
		}
	}
}
