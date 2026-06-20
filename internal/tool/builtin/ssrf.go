package builtin

// SSRF protection for web_fetch (docs/threat-model.md §7, G4). A prompt-injected
// model — or a redirect from a fetched page — must not be able to reach the
// cloud metadata endpoint (169.254.169.254), loopback, or internal/private hosts.
//
// Two layers:
//  1. checkFetchURL: a fast pre-flight on the scheme and any literal-IP host.
//  2. ssrfDialControl: a net.Dialer Control hook that validates the ACTUAL
//     resolved IP at connect time — this also defeats DNS-rebinding, where a
//     hostname resolves to a public IP at check time but a private IP at dial.

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"syscall"
)

// EnvWebFetchAllowLocal opts out of SSRF blocking for local development (allows
// loopback/private targets, e.g. a local dev server). Off by default.
const EnvWebFetchAllowLocal = "LUMEN_WEBFETCH_ALLOW_LOCAL"

func allowLocalFetch() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvWebFetchAllowLocal))) {
	case "1", "true", "on", "yes":
		return true
	}
	return false
}

// cgnat is the carrier-grade NAT range (RFC 6598), commonly internal.
var cgnat = func() *net.IPNet {
	_, n, _ := net.ParseCIDR("100.64.0.0/10")
	return n
}()

// isBlockedIP reports whether an IP is in a range web_fetch must not reach.
func isBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true // unparseable → fail closed
	}
	if ip.IsLoopback() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsPrivate() {
		return true
	}
	if v4 := ip.To4(); v4 != nil && cgnat.Contains(v4) {
		return true
	}
	return false
}

// ssrfDialControl is a net.Dialer.Control hook. address is the concrete "ip:port"
// the dialer is about to connect to (post-resolution), so checking here is
// rebinding-safe.
func ssrfDialControl(network, address string, _ syscall.RawConn) error {
	if allowLocalFetch() {
		return nil
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		host = address
	}
	ip := net.ParseIP(host)
	if isBlockedIP(ip) {
		return fmt.Errorf("blocked: refusing to connect to non-public address %s (SSRF protection; set %s=1 to allow local targets)", host, EnvWebFetchAllowLocal)
	}
	return nil
}

// checkFetchURL is the pre-flight validation: only http/https, and reject a
// literal-IP host that's already known-bad before any DNS/connect happens.
func checkFetchURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}
	switch u.Scheme {
	case "http", "https":
	default:
		return fmt.Errorf("blocked: scheme %q not allowed (only http/https)", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("invalid url: missing host")
	}
	if allowLocalFetch() {
		return nil
	}
	// If the host is a literal IP, classify it now for a clearer early error.
	if ip := net.ParseIP(host); ip != nil && isBlockedIP(ip) {
		return fmt.Errorf("blocked: refusing to fetch non-public address %s (SSRF protection; set %s=1 to allow local targets)", host, EnvWebFetchAllowLocal)
	}
	return nil
}
