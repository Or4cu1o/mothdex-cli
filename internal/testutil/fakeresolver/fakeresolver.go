// Package fakeresolver is a scripted DNS resolver for tests of the SSRF guards
// (T-16 case 47): a public-looking host name can be made to resolve to a
// loopback or private address.
package fakeresolver

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
)

// Resolver answers LookupIP from a fixed table and counts the lookups.
type Resolver struct {
	mu      sync.Mutex
	records map[string][]net.IP
	lookups []string
}

// New returns a resolver with the given host to address table. Addresses are
// parsed with net.ParseIP and New panics on a bad literal: it is test setup.
func New(table map[string][]string) *Resolver {
	r := &Resolver{records: map[string][]net.IP{}}
	for host, addrs := range table {
		for _, a := range addrs {
			ip := net.ParseIP(a)
			if ip == nil {
				panic(fmt.Sprintf("fakeresolver: bad address %q for %q", a, host))
			}
			r.records[normalize(host)] = append(r.records[normalize(host)], ip)
		}
	}
	return r
}

// LookupIP implements the downloader and cache resolver port. Unknown hosts
// fail like NXDOMAIN, so a test notices an unexpected lookup.
func (r *Resolver) LookupIP(_ context.Context, host string) ([]net.IP, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lookups = append(r.lookups, normalize(host))
	ips, ok := r.records[normalize(host)]
	if !ok {
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	return append([]net.IP(nil), ips...), nil
}

// Lookups returns the host names resolved so far, in order.
func (r *Resolver) Lookups() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.lookups...)
}

func normalize(host string) string {
	return strings.TrimSuffix(strings.ToLower(host), ".")
}
