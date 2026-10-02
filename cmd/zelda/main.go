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

	"git.subcult.tv/subculture-collective/zelda/internal/analytics"
	"git.subcult.tv/subculture-collective/zelda/internal/config"
	"git.subcult.tv/subculture-collective/zelda/internal/db"
	"git.subcult.tv/subculture-collective/zelda/internal/mail"
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

	st := store.New(pool)
	jobs := &analytics.Jobs{Store: st, Mail: sender, Log: log, BaseURL: cfg.BaseURL, AppName: cfg.AppName,
		RetentionDays: cfg.StatsRetentionDays}
	go jobs.Run(ctx, time.Hour)

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           server.New(cfg, st, sender, log, editor).Handler(),
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
