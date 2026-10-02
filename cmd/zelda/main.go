// Command zelda runs the links page service.
package main

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"git.subcult.tv/subculture-collective/zelda/internal/config"
	"git.subcult.tv/subculture-collective/zelda/internal/db"
	"git.subcult.tv/subculture-collective/zelda/internal/mail"
	"git.subcult.tv/subculture-collective/zelda/internal/screen"
	"git.subcult.tv/subculture-collective/zelda/internal/server"
	"git.subcult.tv/subculture-collective/zelda/internal/store"
	"git.subcult.tv/subculture-collective/zelda/web"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool); err != nil {
		return err
	}

	var sender mail.Sender = mail.Log{Logger: log}
	if cfg.SMTPHost != "" {
		sender = mail.SMTP{Host: cfg.SMTPHost, Port: cfg.SMTPPort, User: cfg.SMTPUser, Pass: cfg.SMTPPass, From: cfg.MailFrom}
	} else {
		log.Warn("ZELDA_SMTP_HOST not set: verification and reset emails are only logged")
	}

	var editor fs.FS
	if sub, err := fs.Sub(web.Dist, "dist"); err == nil {
		if _, err := fs.Stat(sub, "index.html"); err == nil {
			editor = sub
		} else {
			log.Warn("editor bundle missing: run `make web` before building")
		}
	}

	app := server.New(cfg, store.New(pool), sender, log, editor)
	if sc := screener(ctx, cfg, log); sc != nil {
		app.SetScreener(sc)
		go app.RunRechecks(ctx, 6*time.Hour)
	}
	if cfg.PublishMinAge > 0 {
		log.Info("new accounts can publish after", "min_age", cfg.PublishMinAge.String())
	}

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           app.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	errc := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.Addr, "base_url", cfg.BaseURL)
		errc <- srv.ListenAndServe()
	}()
	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		log.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
	return nil
}

// screener builds the destination screener from config, or returns nil (and
// warns) when no provider is configured.
func screener(ctx context.Context, cfg *config.Config, log *slog.Logger) screen.Screener {
	var list screen.Multi
	if cfg.SafeBrowsingKey != "" {
		list = append(list, screen.NewSafeBrowsing(cfg.SafeBrowsingKey))
		log.Info("link screening: Google Safe Browsing enabled")
	}
	if cfg.BlocklistFile != "" || cfg.BlocklistURL != "" {
		bl := &screen.Blocklist{File: cfg.BlocklistFile, FeedURL: cfg.BlocklistURL, Log: log}
		if cfg.BlocklistFile != "" {
			if err := bl.Load(); err != nil && cfg.BlocklistURL == "" {
				log.Warn("link screening: cannot read ZELDA_BLOCKLIST_FILE", "err", err)
			}
		}
		go bl.Run(ctx, 24*time.Hour)
		list = append(list, bl)
		log.Info("link screening: blocklist enabled", "file", cfg.BlocklistFile, "feed", cfg.BlocklistURL, "entries", bl.Len())
	}
	if len(list) == 0 {
		log.Warn("link screening off: set ZELDA_SAFE_BROWSING_KEY or ZELDA_BLOCKLIST_FILE/ZELDA_BLOCKLIST_URL to check links for phishing and malware")
		return nil
	}
	return list
}
