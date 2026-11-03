package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"syscall"
	"testing"
	"time"

	"git.subcult.tv/subculture-collective/zelda/internal/db"
)

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

// The server starts from environment configuration, applies every
// migration, answers /healthz (and the -healthcheck probe), and shuts down
// cleanly on SIGTERM.
func TestRunStartsMigratesAndStops(t *testing.T) {
	dsn := os.Getenv("ZELDA_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("ZELDA_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	admin, err := db.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	const schema = "cmd_zelda_test"
	if _, err := admin.Exec(ctx, `DROP SCHEMA IF EXISTS `+schema+` CASCADE; CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	u, _ := url.Parse(dsn)
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()

	addr := freeAddr(t)
	t.Setenv("ZELDA_DATABASE_URL", u.String())
	t.Setenv("ZELDA_ADDR", addr)
	t.Setenv("ZELDA_BASE_URL", "http://"+addr)
	t.Setenv("ZELDA_UPLOAD_DIR", t.TempDir())
	t.Setenv("ZELDA_CUSTOM_DOMAINS", "false")

	errc := make(chan error, 1)
	go func() { errc <- run(slog.New(slog.NewTextHandler(io.Discard, nil))) }()

	deadline := time.Now().Add(20 * time.Second)
	for {
		res, err := http.Get("http://" + addr + "/healthz")
		if err == nil {
			io.Copy(io.Discard, res.Body) //nolint:errcheck
			res.Body.Close()
			if res.StatusCode == http.StatusOK {
				break
			}
		}
		select {
		case err := <-errc:
			t.Fatalf("run exited early: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("server didn't become healthy")
		}
		time.Sleep(100 * time.Millisecond)
	}
	if code := healthcheck(); code != 0 {
		t.Fatalf("healthcheck = %d", code)
	}

	entries, err := os.ReadDir("../../internal/db/migrations")
	if err != nil {
		t.Fatal(err)
	}
	var applied int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM `+schema+`.schema_migrations`).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied != len(entries) {
		t.Fatalf("applied %d of %d migrations", applied, len(entries))
	}

	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errc:
		if err != nil {
			t.Fatalf("shutdown: %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("server didn't stop on SIGTERM")
	}
	if code := healthcheck(); code != 1 {
		t.Fatalf("healthcheck after shutdown = %d", code)
	}
}
