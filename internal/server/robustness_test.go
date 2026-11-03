package server

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"git.subcult.tv/subculture-collective/zelda/internal/config"
	"git.subcult.tv/subculture-collective/zelda/internal/mail"
)

func TestClientIP(t *testing.T) {
	cases := []struct {
		name   string
		cfg    config.Config
		xff    []string
		header string
		want   string
	}{
		{"no proxy ignores headers", config.Config{}, []string{"1.1.1.1"}, "", "192.0.2.9"},
		{"legacy leftmost", config.Config{TrustProxy: true}, []string{"6.6.6.6, 1.1.1.1"}, "", "6.6.6.6"},
		{"one appending proxy", config.Config{TrustProxy: true, ProxyHops: 1}, []string{"6.6.6.6, 1.1.1.1"}, "", "1.1.1.1"},
		{"two proxies", config.Config{TrustProxy: true, ProxyHops: 2}, []string{"6.6.6.6, 1.1.1.1", "10.0.0.5"}, "", "1.1.1.1"},
		{"hops beyond the list", config.Config{TrustProxy: true, ProxyHops: 3}, []string{"1.1.1.1"}, "", "1.1.1.1"},
		{"client IP header wins", config.Config{TrustProxy: true, ClientIPHeader: "CF-Connecting-IP"}, []string{"6.6.6.6"}, "1.1.1.1", "1.1.1.1"},
		{"missing client IP header", config.Config{TrustProxy: true, ClientIPHeader: "CF-Connecting-IP"}, []string{"6.6.6.6"}, "", "192.0.2.9"},
	}
	for _, c := range cases {
		cfg := c.cfg
		s := &Server{cfg: &cfg}
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = "192.0.2.9:4000"
		for _, v := range c.xff {
			r.Header.Add("X-Forwarded-For", v)
		}
		if c.header != "" {
			r.Header.Set("CF-Connecting-IP", c.header)
		}
		if got := s.clientIP(r); got != c.want {
			t.Errorf("%s: got %s, want %s", c.name, got, c.want)
		}
	}
}

type slowMail struct{ sent atomic.Int32 }

func (m *slowMail) Send(context.Context, mail.Message) error {
	time.Sleep(100 * time.Millisecond)
	m.sent.Add(1)
	return nil
}

// Shutdown waits for queued emails (#62).
func TestWaitMail(t *testing.T) {
	m := &slowMail{}
	s := &Server{mail: m, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	s.sendMail(mail.Message{To: "a@example.org"})
	s.sendMail(mail.Message{To: "b@example.org"})
	if err := s.WaitMail(context.Background()); err != nil || m.sent.Load() != 2 {
		t.Fatalf("waited: %v, sent %d", err, m.sent.Load())
	}
	s.sendMail(mail.Message{To: "c@example.org"})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := s.WaitMail(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expired wait: %v", err)
	}
	s.WaitMail(context.Background()) //nolint:errcheck
}

// A save and a refresh of the same latest block don't run at once (#65).
func TestFeedGuardSerializes(t *testing.T) {
	s := &Server{x: newBlockExtras(&config.Config{})}
	var running, overlap atomic.Int32
	work := func() {
		if running.Add(1) > 1 {
			overlap.Add(1)
		}
		time.Sleep(20 * time.Millisecond)
		running.Add(-1)
	}
	done := make(chan struct{})
	for range 4 {
		go func() {
			s.withFeedGuard(context.Background(), "block", work)
			done <- struct{}{}
		}()
	}
	for range 4 {
		<-done
	}
	if overlap.Load() != 0 {
		t.Fatalf("%d overlapping runs", overlap.Load())
	}
	// A refresh started meanwhile shares the same guard.
	s.x.inflight["block"] = make(chan struct{})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	ran := false
	s.withFeedGuard(ctx, "block", func() { ran = true })
	if ran {
		t.Fatal("ran while a refresh held the guard")
	}
}
