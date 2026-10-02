package screen

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// maxFeedBytes caps a downloaded or loaded blocklist.
const maxFeedBytes = 64 << 20

// Blocklist flags URLs whose host (or a parent domain of it) is listed, or
// whose exact URL is listed.
//
// The list has one entry per line. Blank lines and lines starting with # are
// skipped. An entry is either a hostname ("evil.example"), a hosts-file line
// ("0.0.0.0 evil.example", as in the URLhaus host file), or a full http(s) URL
// ("https://host/path", as in the OpenPhish feed). URL entries only match that
// URL, so a phishing page on a shared host doesn't block the whole host.
type Blocklist struct {
	// File is read at startup and rewritten after each successful download.
	File string
	// FeedURL, if set, is downloaded by Run.
	FeedURL string
	Client  *http.Client
	Log     *slog.Logger

	mu    sync.RWMutex
	hosts map[string]bool
	urls  map[string]bool
}

// NewBlocklist returns a blocklist holding entries (useful for tests).
func NewBlocklist(entries ...string) *Blocklist {
	b := &Blocklist{}
	b.set(parseList(strings.NewReader(strings.Join(entries, "\n"))))
	return b
}

// Len returns the number of entries loaded.
func (b *Blocklist) Len() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.hosts) + len(b.urls)
}

func (b *Blocklist) Check(_ context.Context, urls []string) (map[string]Verdict, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make(map[string]Verdict, len(urls))
	for _, u := range urls {
		v := Verdict{}
		if b.urls[normalizeURL(u)] {
			v = Verdict{Unsafe: true, Threat: "blocklist", Source: "blocklist"}
		}
		for _, h := range parents(Hostname(u)) {
			if !v.Unsafe && b.hosts[h] {
				v = Verdict{Unsafe: true, Threat: "blocklist", Source: "blocklist"}
			}
		}
		out[u] = v
	}
	return out, nil
}

// Load reads File. A missing file is not an error when FeedURL is set.
func (b *Blocklist) Load() error {
	f, err := os.Open(b.File)
	if err != nil {
		return err
	}
	defer f.Close()
	list := parseList(io.LimitReader(f, maxFeedBytes))
	b.set(list)
	return nil
}

// Refresh downloads FeedURL, replaces the in-memory list and, when File is
// set, saves the download there.
func (b *Blocklist) Refresh(ctx context.Context) error {
	client := b.Client
	if client == nil {
		client = SafeClient(2 * time.Minute)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.FeedURL, nil)
	if err != nil {
		return err
	}
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("blocklist download: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("blocklist download: HTTP %d", res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, maxFeedBytes+1))
	if err != nil {
		return fmt.Errorf("blocklist download: %w", err)
	}
	if len(data) > maxFeedBytes {
		return errors.New("blocklist download: larger than 64 MB")
	}
	list := parseList(bytes.NewReader(data))
	if len(list.hosts)+len(list.urls) == 0 {
		return errors.New("blocklist download: no entries, keeping the current list")
	}
	b.set(list)
	if b.File != "" {
		tmp := b.File + ".tmp"
		if err := os.MkdirAll(filepath.Dir(b.File), 0o750); err != nil {
			return err
		}
		if err := os.WriteFile(tmp, data, 0o640); err != nil {
			return err
		}
		return os.Rename(tmp, b.File)
	}
	return nil
}

// Run refreshes from FeedURL now (unless File is fresher than every) and then
// every interval until ctx is done. Without a FeedURL it reloads File instead.
func (b *Blocklist) Run(ctx context.Context, every time.Duration) {
	log := b.Log
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	tick := func() {
		if b.FeedURL == "" {
			if err := b.Load(); err != nil {
				log.Warn("blocklist reload failed", "err", err)
			}
			return
		}
		if err := b.Refresh(ctx); err != nil {
			log.Warn("blocklist refresh failed", "err", err)
			return
		}
		log.Info("blocklist refreshed", "entries", b.Len())
	}
	if b.FeedURL != "" {
		st, err := os.Stat(b.File)
		if b.File == "" || err != nil || time.Since(st.ModTime()) > every {
			tick()
		}
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			tick()
		}
	}
}

type parsedList struct {
	hosts map[string]bool
	urls  map[string]bool
}

func (b *Blocklist) set(l parsedList) {
	b.mu.Lock()
	b.hosts, b.urls = l.hosts, l.urls
	b.mu.Unlock()
}

func parseList(r io.Reader) parsedList {
	l := parsedList{hosts: map[string]bool{}, urls: map[string]bool{}}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 64<<10)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		fields := strings.Fields(line)
		entry := fields[0]
		// Hosts-file format: "0.0.0.0 host" or "127.0.0.1 host".
		if len(fields) >= 2 && (entry == "0.0.0.0" || entry == "127.0.0.1" || entry == "::1" || entry == "::") {
			entry = fields[1]
		}
		if strings.Contains(entry, "://") {
			if n := normalizeURL(entry); n != "" {
				l.urls[n] = true
			}
			continue
		}
		host := strings.TrimSuffix(strings.ToLower(entry), ".")
		if host == "" || host == "localhost" || strings.ContainsAny(host, "/?#@ ") {
			continue
		}
		l.hosts[host] = true
	}
	return l
}

// normalizeURL lowercases the scheme and host and drops the fragment and a
// bare trailing slash, so equivalent spellings compare equal.
func normalizeURL(raw string) string {
	p, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (p.Scheme != "http" && p.Scheme != "https") || p.Host == "" {
		return ""
	}
	p.Scheme = "https" // http and https variants of a listed page are the same threat
	p.Host = strings.TrimSuffix(strings.ToLower(p.Host), ".")
	p.Fragment = ""
	p.RawFragment = ""
	if p.Path == "/" {
		p.Path = ""
		p.RawPath = ""
	}
	return p.String()
}
