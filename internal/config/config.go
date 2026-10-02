// Package config reads settings from the environment.
package config

import (
	"errors"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Addr        string // listen address
	DatabaseURL string
	// BaseURL is the public origin of the service, e.g. https://links.subcult.tv.
	BaseURL   string
	BaseHost  string
	AppName   string
	UploadDir string
	// DomainTarget is the hostname custom domains should CNAME to.
	DomainTarget string
	// TrustProxy uses X-Forwarded-For for client IPs (set behind Caddy).
	TrustProxy bool
	// SecureCookies sets the Secure flag; on unless BaseURL is http.
	SecureCookies bool
	// AdminEmails grants admin to these addresses once they are verified.
	AdminEmails map[string]bool
	// CustomDomains enables attaching user domains (needs on-demand TLS in front).
	CustomDomains bool
	// ContactEmail is shown on the terms and privacy pages.
	ContactEmail string

	SMTPHost string
	SMTPPort int
	SMTPUser string
	SMTPPass string
	MailFrom string
}

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// Load reads ZELDA_* environment variables.
func Load() (*Config, error) {
	c := &Config{
		Addr:         env("ZELDA_ADDR", ":8080"),
		DatabaseURL:  env("ZELDA_DATABASE_URL", ""),
		BaseURL:      strings.TrimSuffix(env("ZELDA_BASE_URL", "http://localhost:8080"), "/"),
		AppName:      env("ZELDA_APP_NAME", "Kork"),
		UploadDir:    env("ZELDA_UPLOAD_DIR", "./data/uploads"),
		DomainTarget: env("ZELDA_DOMAIN_TARGET", ""),
		TrustProxy:   env("ZELDA_TRUST_PROXY", "false") == "true",
		SMTPHost:     env("ZELDA_SMTP_HOST", ""),
		SMTPUser:     env("ZELDA_SMTP_USER", ""),
		SMTPPass:     env("ZELDA_SMTP_PASSWORD", ""),
		MailFrom:     env("ZELDA_MAIL_FROM", ""),
		ContactEmail: env("ZELDA_CONTACT_EMAIL", "hello@subcult.tv"),
	}
	c.CustomDomains = env("ZELDA_CUSTOM_DOMAINS", "true") == "true"
	c.AdminEmails = map[string]bool{}
	for _, e := range strings.FieldsFunc(env("ZELDA_ADMIN_EMAILS", ""), func(r rune) bool { return r == ',' || r == ' ' }) {
		c.AdminEmails[strings.ToLower(e)] = true
	}
	port, err := strconv.Atoi(env("ZELDA_SMTP_PORT", "587"))
	if err != nil {
		return nil, errors.New("ZELDA_SMTP_PORT must be a number")
	}
	c.SMTPPort = port
	if c.DatabaseURL == "" {
		return nil, errors.New("ZELDA_DATABASE_URL is required")
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, errors.New("ZELDA_BASE_URL must be an absolute http(s) URL")
	}
	c.BaseHost = strings.ToLower(u.Host)
	c.SecureCookies = u.Scheme == "https"
	if c.DomainTarget == "" {
		c.DomainTarget = u.Hostname()
	}
	if c.SMTPHost != "" && c.MailFrom == "" {
		return nil, errors.New("ZELDA_MAIL_FROM is required when ZELDA_SMTP_HOST is set")
	}
	return c, nil
}
