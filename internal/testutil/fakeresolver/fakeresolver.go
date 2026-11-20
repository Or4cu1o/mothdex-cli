// Package fakeresolver is a scripted DNS resolver for tests of the SSRF guards
// (T-16 case 47): a public-looking host name can be made to resolve to a
// loopback or private address.
package fakeresolver

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"sync"
)

// Resolver answers LookupIP from a fixed table and counts the lookups.
type Resolver struct {
	mu      sync.Mutex
	records map[string][]net.IP
	steps   map[string][][]net.IP // DNS-rebinding mode: one answer per lookup
	lookups []string
}

// NewSequence returns a resolver that answers each lookup of a host with the
// next address set in its list (the last set repeats). It models DNS
// rebinding: the first answer public, the next one private (T-16 case 47).
func NewSequence(table map[string][][]string) *Resolver {
	r := &Resolver{records: map[string][]net.IP{}, steps: map[string][][]net.IP{}}
	for host, sets := range table {
		for _, set := range sets {
			r.steps[normalize(host)] = append(r.steps[normalize(host)], parse(host, set))
		}
	}
	return r
}

// New returns a resolver with the given host to address table. Addresses are
// parsed with net.ParseIP and New panics on a bad literal: it is test setup.
func New(table map[string][]string) *Resolver {
	r := &Resolver{records: map[string][]net.IP{}, steps: map[string][][]net.IP{}}
	for host, addrs := range table {
		r.records[normalize(host)] = parse(host, addrs)
	}
	return r
}

func parse(host string, addrs []string) []net.IP {
	ips := make([]net.IP, 0, len(addrs))
	for _, a := range addrs {
		ip := net.ParseIP(a)
		if ip == nil {
			panic(fmt.Sprintf("fakeresolver: bad address %q for %q", a, host))
		}
		ips = append(ips, ip)
	}
	return ips
}

// LookupIP implements the downloader and cache resolver port. Unknown hosts
// fail like NXDOMAIN, so a test notices an unexpected lookup.
func (r *Resolver) LookupIP(_ context.Context, host string) ([]net.IP, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := normalize(host)
	r.lookups = append(r.lookups, key)
	ips, ok := r.records[key]
	if sets := r.steps[key]; len(sets) > 0 {
		n := 0
		for _, l := range r.lookups {
			if l == key {
				n++
			}
		}
		ips, ok = sets[min(n, len(sets))-1], true
	}
	if !ok {
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	return append([]net.IP(nil), ips...), nil
}

// LookupNetIP matches the *net.Resolver method, so a port written against it
// can take this double or net.DefaultResolver alike. The network argument is
// ignored: every address in the table is returned.
func (r *Resolver) LookupNetIP(ctx context.Context, _, host string) ([]netip.Addr, error) {
	ips, err := r.LookupIP(ctx, host)
	if err != nil {
		return nil, err
	}
	addrs := make([]netip.Addr, 0, len(ips))
	for _, ip := range ips {
		if a, ok := netip.AddrFromSlice(ip); ok {
			addrs = append(addrs, a.Unmap())
		}
	}
	return addrs, nil
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
