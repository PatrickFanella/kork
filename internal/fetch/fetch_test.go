package fetch

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

func TestIsPublic(t *testing.T) {
	blocked := []string{
		"127.0.0.1", "127.8.9.10", "::1", "0.0.0.0", "::",
		"10.0.0.50", "172.16.0.1", "172.31.255.255", "192.168.1.1",
		"169.254.169.254", "169.254.0.1", "fe80::1",
		"100.64.0.1", "100.127.255.254",
		"fc00::1", "fd00:ec2::254", "fdaa::1",
		"224.0.0.1", "ff02::1", "255.255.255.255", "240.0.0.1",
		"::ffff:127.0.0.1", "::ffff:10.0.0.1", "::ffff:169.254.169.254",
		"64:ff9b::a00:1", "2002:a00:1::1", "2001:db8::1", "192.0.2.10",
	}
	for _, s := range blocked {
		if IsPublic(netip.MustParseAddr(s)) {
			t.Errorf("%s should be refused", s)
		}
	}
	allowed := []string{"93.184.216.34", "1.1.1.1", "8.8.8.8", "2606:4700:4700::1111", "::ffff:8.8.8.8", "100.128.0.1", "172.32.0.1"}
	for _, s := range allowed {
		if !IsPublic(netip.MustParseAddr(s)) {
			t.Errorf("%s should be allowed", s)
		}
	}
}

func TestCheckURL(t *testing.T) {
	c := New(Options{})
	bad := []string{
		"ftp://example.com/", "file:///etc/passwd", "gopher://example.com/",
		"http://user:pw@example.com/", "https://example.com:8443/", "http://example.com:22/",
		"http://127.0.0.1/", "http://[::1]/", "http://169.254.169.254/latest/meta-data/",
		"http:///nohost",
	}
	for _, s := range bad {
		u, _ := url.Parse(s)
		if err := c.CheckURL(u); err == nil {
			t.Errorf("%s should be refused", s)
		}
	}
	for _, s := range []string{"https://example.com/", "http://example.com:80/x", "https://example.com:443/"} {
		u, _ := url.Parse(s)
		if err := c.CheckURL(u); err != nil {
			t.Errorf("%s: %v", s, err)
		}
	}
}

// The default policy must refuse httptest servers, which listen on loopback,
// even when the port check is relaxed. This exercises the dial-time check.
func TestRefusesLoopbackAtDial(t *testing.T) {
	hits := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++ }))
	defer ts.Close()
	c := New(Options{AllowPort: func(int) bool { return true }})

	// "localhost" is a hostname, so only the dial-time check can catch it.
	u, _ := url.Parse(ts.URL)
	for _, target := range []string{ts.URL, "http://localhost:" + u.Port() + "/"} {
		_, err := c.Get(context.Background(), target, "", 1024)
		if !errors.Is(err, ErrBlocked) {
			t.Errorf("%s: want ErrBlocked, got %v", target, err)
		}
	}
	if hits != 0 {
		t.Fatalf("server was reached %d times", hits)
	}
}

// A public-looking first hop that redirects to a private address must be
// refused on the redirect.
func TestRefusesRedirectToBlockedAddress(t *testing.T) {
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("internal server reached")
	}))
	defer internal.Close()
	internalPort := netip.MustParseAddrPort(strings.TrimPrefix(internal.URL, "http://")).Port()

	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, internal.URL+"/secret", http.StatusFound)
	}))
	defer front.Close()
	frontPort := netip.MustParseAddrPort(strings.TrimPrefix(front.URL, "http://")).Port()

	// Treat only the front server as "public": loopback is allowed, but the
	// internal server's port is not, standing in for a private address.
	c := New(Options{
		AllowAddr: func(netip.Addr) bool { return true },
		AllowPort: func(p int) bool { return p == int(frontPort) },
	})
	_, err := c.Get(context.Background(), front.URL, "", 1024)
	if !errors.Is(err, ErrURL) {
		t.Fatalf("want ErrURL for redirect to port %d, got %v", internalPort, err)
	}

	// A hostname redirect target passes the URL check, so only the dial-time
	// address check can refuse it. The front server flips the policy before
	// redirecting, standing in for a host that resolves to a private address.
	var blockNow atomic.Bool
	front2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		blockNow.Store(true)
		http.Redirect(w, r, fmt.Sprintf("http://localhost:%d/secret", internalPort), http.StatusFound)
	}))
	defer front2.Close()
	c = New(Options{
		AllowAddr: func(netip.Addr) bool { return !blockNow.Load() },
		AllowPort: func(int) bool { return true },
	})
	if _, err := c.Get(context.Background(), front2.URL, "", 1024); !errors.Is(err, ErrBlocked) {
		t.Fatalf("want ErrBlocked on redirect dial, got %v", err)
	}
}

func testClient() *Client {
	return New(Options{AllowAddr: func(netip.Addr) bool { return true }, AllowPort: func(int) bool { return true }})
}

func TestRedirectLimit(t *testing.T) {
	var ts *httptest.Server
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, ts.URL+"/again", http.StatusFound)
	}))
	defer ts.Close()
	if _, err := testClient().Get(context.Background(), ts.URL, "", 1024); !errors.Is(err, ErrRedirects) {
		t.Fatalf("want ErrRedirects, got %v", err)
	}
}

func TestRedirectsFollowedWithinLimit(t *testing.T) {
	var ts *httptest.Server
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			http.Redirect(w, r, "/a", http.StatusFound)
		case "/a":
			http.Redirect(w, r, "/b", http.StatusMovedPermanently)
		default:
			if got := r.Header.Get("User-Agent"); got != DefaultUserAgent {
				t.Errorf("user agent %q", got)
			}
			w.Header().Set("Content-Type", "text/HTML; charset=utf-8")
			w.Write([]byte("<p>ok</p>"))
		}
	}))
	defer ts.Close()
	res, err := testClient().Get(context.Background(), ts.URL, "text/html", 1024)
	if err != nil {
		t.Fatal(err)
	}
	if res.URL.Path != "/b" || res.MediaType != "text/html" || string(res.Body) != "<p>ok</p>" {
		t.Fatalf("unexpected response: %s %s %q", res.URL, res.MediaType, res.Body)
	}
}

func TestSizeAndStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/big":
			w.Write([]byte(strings.Repeat("x", 2000)))
		case "/chunked":
			w.(http.Flusher).Flush() // no Content-Length
			w.Write([]byte(strings.Repeat("x", 2000)))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()
	c := testClient()
	for _, p := range []string{"/big", "/chunked"} {
		if _, err := c.Get(context.Background(), ts.URL+p, "", 1000); !errors.Is(err, ErrTooLarge) {
			t.Errorf("%s: want ErrTooLarge, got %v", p, err)
		}
	}
	if _, err := c.Get(context.Background(), ts.URL+"/missing", "", 1000); !errors.Is(err, ErrStatus) {
		t.Errorf("want ErrStatus, got %v", err)
	}
}
