package cover

import (
	"net"
	"net/netip"
	"syscall"
)

// blockedPrefixes are special-use ranges that netip's own predicates do not cover: CGNAT,
// IETF protocol assignments, benchmarking, reserved/broadcast, and the IPv6 forms that embed an
// IPv4 address (NAT64, 6to4, Teredo).
var blockedPrefixes = mustPrefixes(
	"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "198.18.0.0/15", "240.0.0.0/4",
	"64:ff9b:1::/48", "2002::/16", "2001::/32", "100::/64", "fec0::/10", "::/96",
)

var nat64 = netip.MustParsePrefix("64:ff9b::/96")

func mustPrefixes(ss ...string) []netip.Prefix {
	out := make([]netip.Prefix, len(ss))
	for i, s := range ss {
		out[i] = netip.MustParsePrefix(s)
	}
	return out
}

// blockedIP reports whether a cover fetch must never connect to addr: loopback, private,
// link-local, CGNAT, multicast, unspecified and the other special-use ranges. An IPv4-mapped
// IPv6 address is judged as its IPv4 form.
func blockedIP(addr netip.Addr) bool {
	addr = addr.Unmap().WithZone("")
	if nat64.Contains(addr) {
		// DNS64 (IPv6-only networks) writes a public IPv4 host as 64:ff9b::a.b.c.d: judge the
		// embedded IPv4 address, so covers still load there but 64:ff9b::7f00:1 stays blocked.
		b := addr.As16()
		return blockedIP(netip.AddrFrom4([4]byte(b[12:])))
	}
	if !addr.IsValid() || addr.IsLoopback() || addr.IsPrivate() || addr.IsUnspecified() ||
		addr.IsLinkLocalUnicast() || addr.IsMulticast() || addr.IsLinkLocalMulticast() {
		return true
	}
	for _, p := range blockedPrefixes {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// control is the dialer Control hook: it runs after DNS, on the address the socket is about to
// connect to, so a resolver answer that changed between the check and the dial is still caught.
func control(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return errBlocked
	}
	addr, err := netip.ParseAddr(host)
	if err != nil || blockedIP(addr) {
		return errBlocked
	}
	return nil
}
