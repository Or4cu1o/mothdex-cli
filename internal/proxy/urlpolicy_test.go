package proxy

import (
	"errors"
	"strings"
	"testing"
)

func TestCheckURL(t *testing.T) {
	ok := map[string]string{
		"https://proxy.mothdex.net/a/b.zip":      "https://proxy.mothdex.net/a/b.zip",
		"https://proxy.mothdex.net./a":           "https://proxy.mothdex.net/a",
		"https://PROXY.MOTHDEX.NET/a":            "https://proxy.mothdex.net/a",
		"https://proxy.mothdex.net:443/a?x=1#fr": "https://proxy.mothdex.net/a?x=1",
	}
	for in, want := range ok {
		u, err := CheckURL(in, DefaultAllowedHosts)
		if err != nil || u.String() != want {
			t.Errorf("%q: got %v, %v, want %q", in, u, err, want)
		}
	}
	bad := []string{
		"http://proxy.mothdex.net/a", "https://proxy.mothdex.net:8443/a", "https://proxy.mothdex.net@evil.test/a",
		"https://user:pw@proxy.mothdex.net/a", "https://@proxy.mothdex.net/a", "https://evil.test/a",
		"https://127.0.0.1/a", "https://[::1]/a", "https://[::1%25eth0]/a", "https://2130706433/a",
		"https://proxy.mothdex.net.evil.test/a", "https://xn--proxy-mothdex.net/a", "https://prоxy.mothdex.net/a",
		"https://proxy.mothdex.net../a", "https://.proxy.mothdex.net/a", "ftp://proxy.mothdex.net/a",
		"//proxy.mothdex.net/a", "https:proxy.mothdex.net", "", "https://",
		"HTTP://proxy.mothdex.net/a", "https://sub.proxy.mothdex.net/a", "\x00https://proxy.mothdex.net",
	}
	for _, in := range bad {
		_, err := CheckURL(in, DefaultAllowedHosts)
		var e *Error
		if !errors.As(err, &e) || e.Code != CodeUntrustedURL {
			t.Errorf("%q must be untrusted_url, got %v", in, err)
		}
	}
}

func TestCheckURLErrorsNeverEchoTheInput(t *testing.T) {
	_, err := CheckURL("https://user:secret-token@evil.test/p", DefaultAllowedHosts)
	if err == nil || strings.Contains(err.Error(), "secret-token") || strings.Contains(err.Error(), "evil.test") {
		t.Fatalf("error leaks the URL: %v", err)
	}
}

func TestCheckURLAllowlistIsNormalizedAndOptional(t *testing.T) {
	if _, err := CheckURL("https://proxy.mothdex.net/a", []string{"Proxy.Mothdex.Net."}); err != nil {
		t.Fatalf("configured host is normalized too: %v", err)
	}
	if _, err := CheckURL("https://proxy.mothdex.net/a", nil); err == nil {
		t.Fatal("an empty allowlist allows nothing")
	}
	if _, err := CheckURL("https://127.0.0.1/a", []string{"127.0.0.1"}); err == nil {
		t.Fatal("an IP literal never passes, even if configured")
	}
}
