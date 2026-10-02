// Package screen checks link destinations against phishing and malware lists.
//
// Only destination URLs are sent to providers. Nothing about visitors is.
package screen

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"sync"
)

// Verdict is the result for one URL. Safe URLs have Unsafe false.
type Verdict struct {
	Unsafe bool
	// Threat names the category, e.g. "SOCIAL_ENGINEERING" or "blocklist".
	Threat string
	// Source names the provider that flagged the URL.
	Source string
}

// Screener checks URLs. The returned map holds an entry for every URL the
// screener could decide. On error the map may be partial; unsafe entries in it
// are still reliable.
type Screener interface {
	Check(ctx context.Context, urls []string) (map[string]Verdict, error)
}

// Multi combines screeners: a URL is unsafe if any of them flags it.
type Multi []Screener

func (m Multi) Check(ctx context.Context, urls []string) (map[string]Verdict, error) {
	out := make(map[string]Verdict, len(urls))
	for _, u := range urls {
		out[u] = Verdict{}
	}
	var errs []error
	for _, s := range m {
		res, err := s.Check(ctx, urls)
		if err != nil {
			errs = append(errs, err)
		}
		for u, v := range res {
			if v.Unsafe && !out[u].Unsafe {
				out[u] = v
			}
		}
	}
	return out, errors.Join(errs...)
}

// Hostname returns the lowercased host of an http(s) URL, or "" otherwise.
func Hostname(raw string) string {
	p, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (p.Scheme != "http" && p.Scheme != "https") {
		return ""
	}
	return strings.TrimSuffix(strings.ToLower(p.Hostname()), ".")
}

// Fake is an in-memory screener for tests. Hosts listed in Unsafe (and their
// subdomains) are flagged with the mapped threat.
type Fake struct {
	mu     sync.Mutex
	Unsafe map[string]string
	Err    error
	// Calls counts URLs passed to Check.
	Calls int
}

func (f *Fake) Check(_ context.Context, urls []string) (map[string]Verdict, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls += len(urls)
	if f.Err != nil {
		return nil, f.Err
	}
	out := make(map[string]Verdict, len(urls))
	for _, u := range urls {
		for _, h := range parents(Hostname(u)) {
			if threat, ok := f.Unsafe[h]; ok {
				out[u] = Verdict{Unsafe: true, Threat: threat, Source: "fake"}
				break
			}
		}
		if !out[u].Unsafe {
			out[u] = Verdict{}
		}
	}
	return out, nil
}

// CallCount returns how many URLs have been checked.
func (f *Fake) CallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Calls
}

// SetErr makes Check fail with err (nil to recover).
func (f *Fake) SetErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Err = err
}

// Flag marks host unsafe (safe for concurrent use with Check).
func (f *Fake) Flag(host, threat string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Unsafe == nil {
		f.Unsafe = map[string]string{}
	}
	f.Unsafe[host] = threat
}

// parents returns host and each parent domain: a.b.c, b.c, c.
func parents(host string) []string {
	if host == "" {
		return nil
	}
	out := []string{host}
	for i := 0; i < len(host); i++ {
		if host[i] == '.' && i+1 < len(host) {
			out = append(out, host[i+1:])
		}
	}
	return out
}
