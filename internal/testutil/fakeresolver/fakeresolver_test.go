package fakeresolver_test

import (
	"errors"
	"net"
	"testing"

	"github.com/Or4cu1o/mothdex-cli/internal/testutil/fakeresolver"
)

func TestResolvesPublicNameToLoopback(t *testing.T) {
	r := fakeresolver.New(map[string][]string{"Cdn.Example.Com": {"127.0.0.1", "::1"}})
	ips, err := r.LookupIP(t.Context(), "cdn.example.com.") // case and trailing dot ignored
	if err != nil || len(ips) != 2 || !ips[0].Equal(net.ParseIP("127.0.0.1")) {
		t.Fatalf("ips = %v, err = %v", ips, err)
	}
	if got := r.Lookups(); len(got) != 1 || got[0] != "cdn.example.com" {
		t.Fatalf("lookups = %v", got)
	}
}

func TestUnknownHostFailsLikeNXDOMAIN(t *testing.T) {
	_, err := fakeresolver.New(nil).LookupIP(t.Context(), "nope.test")
	var dnsErr *net.DNSError
	if !errors.As(err, &dnsErr) || !dnsErr.IsNotFound {
		t.Fatalf("err = %v", err)
	}
}
