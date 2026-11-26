package update

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"
)

// Verifier checks release origin independently of the mandatory archive hashes.
type Verifier interface {
	Verify(context.Context, VerifyInput) (string, error)
}

// VerifyInput binds checksums and every downloaded archive to an exact release tag.
type VerifyInput struct {
	Tag               string
	Checksums, Bundle []byte
	Archives          map[string]string
}
type toolVerifier struct{ lookPath func(string) (string, error) }

// DefaultVerifier selects cosign first, then gh attestation, else reports SHA-256 only.
func DefaultVerifier(lookPath func(string) (string, error)) Verifier {
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	return toolVerifier{lookPath}
}
func (v toolVerifier) Verify(ctx context.Context, r VerifyInput) (string, error) {
	if len(r.Tag) < 2 || r.Tag[0] != 'v' || !validVersion(r.Tag[1:]) {
		return "", fmt.Errorf("invalid verification tag")
	}
	for _, tool := range []string{"cosign", "gh"} {
		path, err := v.lookPath(tool)
		if err != nil {
			continue
		}
		if !filepath.IsAbs(path) {
			return "", fmt.Errorf("verifier path is not absolute")
		}
		ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		if tool == "cosign" {
			if len(r.Bundle) == 0 {
				return "", fmt.Errorf("missing signature bundle")
			}
			dir, err := os.MkdirTemp("", "mothdex-verify-")
			if err != nil {
				return "", err
			}
			defer func() { _ = os.RemoveAll(dir) }()
			sums, bundle := filepath.Join(dir, "checksums.txt"), filepath.Join(dir, "checksums.txt.sigstore.json")
			if err = os.WriteFile(sums, r.Checksums, 0600); err != nil {
				return "", err
			}
			if err = os.WriteFile(bundle, r.Bundle, 0600); err != nil {
				return "", err
			}
			args := []string{"verify-blob", "--bundle", bundle, "--certificate-identity", "https://github.com/Or4cu1o/mothdex-cli/.github/workflows/release.yml@refs/tags/" + r.Tag, "--certificate-oidc-issuer", "https://token.actions.githubusercontent.com", sums}
			cmd := exec.CommandContext(ctx, path, args...) //nolint:gosec // absolute LookPath result, fixed argv, validated tag; never a shell.
			cmd.Env = toolEnv()
			if err = cmd.Run(); err != nil {
				return "", err
			}
			return "cosign", nil
		}
		names := make([]string, 0, len(r.Archives))
		for name := range r.Archives {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			cmd := exec.CommandContext(ctx, path, "attestation", "verify", r.Archives[name], "--repo", "Or4cu1o/mothdex-cli", "--signer-workflow", "Or4cu1o/mothdex-cli/.github/workflows/release.yml", "--source-ref", "refs/tags/"+r.Tag) //nolint:gosec // absolute LookPath result, fixed argv, validated tag; never a shell.
			cmd.Env = toolEnv()
			if err = cmd.Run(); err != nil {
				return "", err
			}
		}
		return "attestation", nil
	}
	return "none", nil
}

// Only transport, OS and tool configuration; never forward API credentials or MOTHDEX_*.
func toolEnv() []string {
	var env []string
	for _, key := range []string{"PATH", "HOME", "USERPROFILE", "LOCALAPPDATA", "APPDATA", "SYSTEMROOT", "WINDIR", "TEMP", "TMP", "TMPDIR", "HTTPS_PROXY", "HTTP_PROXY", "ALL_PROXY", "NO_PROXY", "SSL_CERT_FILE", "SSL_CERT_DIR"} {
		if value, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+value)
		}
	}
	return env
}
