package cover

import (
	"net/netip"
	"testing"
)

// T-75 edges around the two exceptions that let an address through the ::/8 block: the unmapped IPv4 form and the
// well-known NAT64 /96 (DNS64). The width of that /96 and the unmapping are the only places a wrong change opens a hole
// or breaks IPv6-only networks.
func TestBlockedIPDNS64AndMappedEdges(t *testing.T) {
	blocked := []string{
		"64:ff9b::1:5db8:d822",    // bits 80-95 set: outside the /96, so not NAT64, even though the last 32 bits are public
		"64:ff9b::ffff:5db8:d822", // same, other value
		"64:ff9b::a00:1",          // embedded 10.0.0.1
		"64:ff9b::c0a8:1",         // embedded 192.168.0.1
		"64:ff9b::a9fe:a9fe",      // embedded 169.254.169.254 (cloud metadata)
		"64:ff9b::6440:1",         // embedded 100.64.0.1 (CGNAT)
		"64:ff9b::e000:1",         // embedded 224.0.0.1
		"64:ff9b::",               // embedded 0.0.0.0
		"::ffff:127.0.0.1",        // mapped loopback
		"::ffff:169.254.169.254",  // mapped metadata
		"::ffff:0:7f00:1",         // IPv4-translated (RFC 2765) loopback
	}
	for _, s := range blocked {
		if !blockedIP(netip.MustParseAddr(s)) {
			t.Errorf("%s must be blocked", s)
		}
	}
	allowed := []string{
		"::ffff:8.8.8.8",   // mapped public address is judged as 8.8.8.8
		"64:ff9b::808:808", // DNS64 of 8.8.8.8
	}
	for _, s := range allowed {
		if blockedIP(netip.MustParseAddr(s)) {
			t.Errorf("%s must be allowed", s)
		}
	}
}
