// Package config reads settings from the environment.
package config

import (
	"errors"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"git.subcult.tv/subculture-collective/zelda/internal/seal"
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
	// StatsRetentionDays is how long aggregate stats rows are kept.
	StatsRetentionDays int
	// ContactEmail is shown on the terms and privacy pages.
	ContactEmail string

	// SafeBrowsingKey enables Google Safe Browsing checks of link destinations.
	SafeBrowsingKey string
	// BlocklistFile is a local host/URL blocklist; BlocklistURL refreshes it daily.
	BlocklistFile string
	BlocklistURL  string
	// PublishMinAge is how old an account must be before it can publish,
	// unless an admin approves it. Zero disables the gate.
	PublishMinAge time.Duration
	// SecretKey (ZELDA_SECRET_KEY, 32 bytes) encrypts stored third-party API
	// keys such as a creator's Brevo key. Nil disables features that need it.
	SecretKey []byte

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
	c.SafeBrowsingKey = env("ZELDA_SAFE_BROWSING_KEY", "")
	c.BlocklistFile = env("ZELDA_BLOCKLIST_FILE", "")
	c.BlocklistURL = env("ZELDA_BLOCKLIST_URL", "")
	if c.BlocklistURL != "" {
		if u, err := url.Parse(c.BlocklistURL); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return nil, errors.New("ZELDA_BLOCKLIST_URL must be an absolute http(s) URL")
		}
	}
	age, err := time.ParseDuration(env("ZELDA_PUBLISH_MIN_ACCOUNT_AGE", "24h"))
	if err != nil || age < 0 {
		return nil, errors.New("ZELDA_PUBLISH_MIN_ACCOUNT_AGE must be a duration such as 24h, or 0 to disable")
	}
	c.PublishMinAge = age
	if v := env("ZELDA_SECRET_KEY", ""); v != "" {
		key, err := seal.ParseKey(v)
		if err != nil {
			return nil, errors.New("ZELDA_SECRET_KEY: " + err.Error() + " (generate one with: openssl rand -base64 32)")
		}
		c.SecretKey = key
	}
	port, err := strconv.Atoi(env("ZELDA_SMTP_PORT", "587"))
	if err != nil {
		return nil, errors.New("ZELDA_SMTP_PORT must be a number")
	}
	c.SMTPPort = port
	c.StatsRetentionDays, err = strconv.Atoi(env("ZELDA_STATS_RETENTION_DAYS", "400"))
	if err != nil || c.StatsRetentionDays < 30 {
		return nil, errors.New("ZELDA_STATS_RETENTION_DAYS must be a number of days, at least 30")
	}
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
