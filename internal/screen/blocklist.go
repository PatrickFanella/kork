package screen

import (
	"bufio"
	"bytes"
	"context"
	"encoding/csv"
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
// PhishTank's online-valid.csv format is also supported; only verified,
// online entries are loaded and they match exact URLs, never entire hosts.
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
	l, _ := parseList(strings.NewReader(strings.Join(entries, "\n")))
	b.set(l)
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
	data, err := io.ReadAll(io.LimitReader(f, maxFeedBytes+1))
	if err != nil {
		return err
	}
	if len(data) > maxFeedBytes {
		return errors.New("blocklist file: larger than 64 MB")
	}
	list, err := parseList(bytes.NewReader(data))
	if err != nil {
		return err
	}
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
	req.Header.Set("User-Agent", "Kork/0.1 (+https://kork.subcult.tv)")
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
	list, err := parseList(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("blocklist download: %w", err)
	}
	if len(list.hosts)+len(list.urls) == 0 {
		return errors.New("blocklist download: no entries, keeping the current list")
	}
	if b.File != "" {
		tmp := b.File + ".tmp"
		if err := os.MkdirAll(filepath.Dir(b.File), 0o750); err != nil {
			return err
		}
		if err := os.WriteFile(tmp, data, 0o640); err != nil {
			return err
		}
		if err := os.Rename(tmp, b.File); err != nil {
			return err
		}
	}
	b.set(list)
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

func parseList(r io.Reader) (parsedList, error) {
	l := parsedList{hosts: map[string]bool{}, urls: map[string]bool{}}
	br := bufio.NewReader(r)
	header, _ := br.Peek(len("phish_id,url,"))
	if string(header) == "phish_id,url," {
		cr := csv.NewReader(br)
		h, err := cr.Read()
		if err != nil || len(h) != 8 || h[4] != "verified" || h[6] != "online" {
			return l, errors.New("invalid PhishTank CSV header")
		}
		for {
			row, err := cr.Read()
			if errors.Is(err, io.EOF) {
				return l, nil
			}
			if err != nil {
				return l, fmt.Errorf("invalid PhishTank CSV: %w", err)
			}
			if row[4] == "yes" && row[6] == "yes" {
				n := normalizeURL(row[1])
				if n == "" {
					// The live feed can contain malformed percent escapes.
					// Skip unusable URLs without discarding valid records.
					continue
				}
				l.urls[n] = true
			}
		}
	}
	sc := bufio.NewScanner(br)
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
		if !validListHost(host) {
			continue
		}
		l.hosts[host] = true
	}
	return l, sc.Err()
}

func validListHost(host string) bool {
	if host == "" || host == "localhost" || !strings.Contains(host, ".") {
		return false
	}
	for _, c := range host {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '.') {
			return false
		}
	}
	return true
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
