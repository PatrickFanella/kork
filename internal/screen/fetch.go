package screen

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"
)

var errBlockedAddr = errors.New("refusing to connect to a private or reserved address")

// cgnat covers 100.64.0.0/10 (carrier-grade NAT and Tailscale).
var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// publicIP reports whether ip is a routable public unicast address.
func publicIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	return ip.IsValid() && ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() &&
		!ip.IsLinkLocalUnicast() && !ip.IsUnspecified() && !cgnat.Contains(ip) &&
		!(ip.Is4() && ip.As4()[0] == 0)
}

// dialControl runs after DNS resolution, so it sees the address actually dialed.
func dialControl(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || !publicIP(ip) {
		return fmt.Errorf("%w: %s", errBlockedAddr, host)
	}
	return nil
}

// SafeClient returns an HTTP client for provider and feed requests. It refuses
// private, loopback, link-local and metadata addresses at dial time, ignores
// proxy settings, follows at most 3 redirects (each dial checked the same
// way) and gives up after timeout.
func SafeClient(timeout time.Duration) *http.Client {
	d := &net.Dialer{Timeout: 10 * time.Second, Control: dialControl}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext:           d.DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 20 * time.Second,
			MaxIdleConns:          4,
			IdleConnTimeout:       90 * time.Second,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("too many redirects")
			}
			if req.URL.Scheme != "https" && req.URL.Scheme != "http" {
				return errors.New("redirect to unsupported scheme")
			}
			return nil
		},
	}
}
