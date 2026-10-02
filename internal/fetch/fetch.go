// Package fetch makes outbound HTTP requests to user-supplied URLs without
// letting them reach the server's own network.
//
// Every connection is checked at dial time against the resolved IP address,
// so DNS rebinding and redirects to internal hosts are refused the same way
// as a literal private address. Only http and https on ports 80 and 443 are
// allowed, redirects are capped and re-checked, and response bodies are
// bounded in time and size.
package fetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	DefaultUserAgent    = "KorkBot/1.0 (+https://kork.subcult.tv)"
	DefaultTimeout      = 8 * time.Second
	DefaultMaxRedirects = 5

	// MaxHTML and MaxImage are the suggested body limits for pages and images.
	MaxHTML  = 2 << 20
	MaxImage = 5 << 20
)

var (
	// ErrBlocked means the destination resolved to an address the policy refuses.
	ErrBlocked = errors.New("destination address is not allowed")
	// ErrURL means the URL itself is unusable: bad scheme, port, host or credentials.
	ErrURL = errors.New("URL is not allowed")
	// ErrTooLarge means the body exceeded the caller's size limit.
	ErrTooLarge = errors.New("response is too large")
	// ErrRedirects means the redirect chain was longer than allowed.
	ErrRedirects = errors.New("too many redirects")
	// ErrStatus means the final response was not 2xx.
	ErrStatus = errors.New("unexpected response status")
)

// Options configures a Client. Zero values select the defaults.
type Options struct {
	UserAgent    string
	Timeout      time.Duration
	MaxRedirects int
	// AllowAddr decides whether a resolved IP may be dialed; nil means IsPublic.
	// Tests replace it because httptest servers listen on loopback.
	AllowAddr func(netip.Addr) bool
	// AllowPort decides whether a URL port may be used; nil means 80 and 443 only.
	AllowPort func(port int) bool
}

// Client fetches user-supplied URLs safely. It is safe for concurrent use.
type Client struct {
	opts Options
	hc   *http.Client
}

// New builds a Client.
func New(opts Options) *Client {
	if opts.UserAgent == "" {
		opts.UserAgent = DefaultUserAgent
	}
	if opts.Timeout <= 0 {
		opts.Timeout = DefaultTimeout
	}
	if opts.MaxRedirects <= 0 {
		opts.MaxRedirects = DefaultMaxRedirects
	}
	if opts.AllowAddr == nil {
		opts.AllowAddr = IsPublic
	}
	if opts.AllowPort == nil {
		opts.AllowPort = func(p int) bool { return p == 80 || p == 443 }
	}
	c := &Client{opts: opts}
	dialer := &net.Dialer{
		Timeout: opts.Timeout,
		// Control runs after DNS resolution with the literal address being
		// dialed, for every address the dialer tries.
		Control: func(network, address string, _ syscall.RawConn) error {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return ErrBlocked
			}
			ip, err := netip.ParseAddr(host)
			if err != nil || !opts.AllowAddr(ip) {
				return ErrBlocked
			}
			if p, err := strconv.Atoi(port); err != nil || !opts.AllowPort(p) {
				return ErrBlocked
			}
			return nil
		},
	}
	transport := &http.Transport{
		Proxy:                  nil, // a proxy would hide the real destination from the dial check
		DialContext:            dialer.DialContext,
		ForceAttemptHTTP2:      true,
		MaxIdleConns:           20,
		IdleConnTimeout:        30 * time.Second,
		TLSHandshakeTimeout:    5 * time.Second,
		ResponseHeaderTimeout:  opts.Timeout,
		MaxResponseHeaderBytes: 64 << 10,
	}
	c.hc = &http.Client{
		Transport: transport,
		Timeout:   opts.Timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > opts.MaxRedirects {
				return ErrRedirects
			}
			return c.CheckURL(req.URL)
		},
	}
	return c
}

// CheckURL validates scheme, credentials, host and port before any request.
// The resolved address is checked separately when the connection is dialed.
func (c *Client) CheckURL(u *url.URL) error {
	if u == nil || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("%w: only http and https links can be fetched", ErrURL)
	}
	if u.User != nil {
		return fmt.Errorf("%w: links with a username or password can't be fetched", ErrURL)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("%w: missing host", ErrURL)
	}
	port := 80
	if u.Scheme == "https" {
		port = 443
	}
	if p := u.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil {
			return fmt.Errorf("%w: invalid port", ErrURL)
		}
		port = n
	}
	if !c.opts.AllowPort(port) {
		return fmt.Errorf("%w: only ports 80 and 443 can be fetched", ErrURL)
	}
	if ip, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil && !c.opts.AllowAddr(ip) {
		return ErrBlocked
	}
	return nil
}

// Response is a fully read, size-limited response.
type Response struct {
	// URL is the final URL after redirects.
	URL *url.URL
	// MediaType is the declared Content-Type without parameters, lowercased.
	MediaType string
	Body      []byte
}

// Get fetches rawURL and reads at most maxBytes of body. accept is sent as
// the Accept header. Non-2xx responses return ErrStatus.
func (c *Client) Get(ctx context.Context, rawURL, accept string, maxBytes int64) (*Response, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil, fmt.Errorf("%w: invalid URL", ErrURL)
	}
	if err := c.CheckURL(u); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid URL", ErrURL)
	}
	req.Header.Set("User-Agent", c.opts.UserAgent)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	res, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return nil, fmt.Errorf("%w: %d", ErrStatus, res.StatusCode)
	}
	if res.ContentLength > maxBytes {
		return nil, ErrTooLarge
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxBytes {
		return nil, ErrTooLarge
	}
	mt, _, _ := mime.ParseMediaType(res.Header.Get("Content-Type"))
	return &Response{URL: res.Request.URL, MediaType: strings.ToLower(mt), Body: body}, nil
}

var blockedPrefixes = func() []netip.Prefix {
	var out []netip.Prefix
	for _, s := range []string{
		"0.0.0.0/8",          // "this" network
		"100.64.0.0/10",      // carrier-grade NAT
		"192.0.0.0/24",       // IETF protocol assignments
		"192.0.2.0/24",       // documentation
		"198.18.0.0/15",      // benchmarking
		"198.51.100.0/24",    // documentation
		"203.0.113.0/24",     // documentation
		"240.0.0.0/4",        // reserved, includes broadcast
		"64:ff9b::/96",       // NAT64 can reach private IPv4
		"64:ff9b:1::/48",     // local-use NAT64
		"100::/64",           // discard
		"2001::/32",          // Teredo
		"2001:db8::/32",      // documentation
		"2002::/16",          // 6to4 can embed private IPv4
		"fec0::/10",          // deprecated site-local
		"169.254.169.254/32", // cloud metadata (also link-local; listed for clarity)
	} {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}()

// IsPublic reports whether ip is a globally routable unicast address. It
// refuses loopback, private (including IPv6 ULA fc00::/7), link-local
// (including cloud metadata 169.254.169.254), CGNAT, multicast, unspecified,
// documentation and translation ranges. IPv4-mapped IPv6 addresses are
// judged by their IPv4 address.
func IsPublic(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || ip.Zone() != "" {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return false
	}
	for _, p := range blockedPrefixes {
		if p.Contains(ip) {
			return false
		}
	}
	return true
}
