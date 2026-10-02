package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"git.subcult.tv/subculture-collective/zelda/internal/store"
)

// domainProvider is how custom domains are registered, verified and removed.
// dnsDomains checks DNS records itself and relies on on-demand TLS in front
// (VPS mode); cloudflareDomains registers Cloudflare for SaaS custom hostnames.
type domainProvider interface {
	// Mode is "dns" or "cloudflare".
	Mode() string
	// Attach runs after the domain row is created. On error the row is deleted.
	Attach(ctx context.Context, d *store.Domain) error
	// Check refreshes d and marks it verified when it is ready. A
	// *domainError tells the user what is missing.
	Check(ctx context.Context, d *store.Domain) error
	// Detach runs before the domain row is deleted; an error keeps the row.
	Detach(ctx context.Context, d *store.Domain) error
	// Release removes provider state for every domain on a page (pageID) or
	// account (userID) that is about to be deleted. Failures are logged.
	Release(ctx context.Context, pageID, userID string)
	// Describe fills in the status, records and errors shown to the user.
	Describe(d *store.Domain, v *domainView)
}

// domainError is a provider failure the user can act on.
type domainError struct {
	status int
	code   string
	msg    string
}

func (e *domainError) Error() string { return e.code + ": " + e.msg }

// writeDomainErr writes a *domainError as-is and anything else as a 500.
func (s *Server) writeDomainErr(w http.ResponseWriter, r *http.Request, err error) {
	var de *domainError
	if errors.As(err, &de) {
		writeErr(w, de.status, de.code, de.msg, "")
		return
	}
	s.serverErr(w, r, err)
}

// dnsRecord is a DNS record the user adds at their DNS provider.
type dnsRecord struct {
	Type  string `json:"type"`
	Name  string `json:"name"`
	Value string `json:"value"`
	// Purpose is "routing", "ownership" or "certificate".
	Purpose string `json:"purpose"`
}

// ---- DNS mode ----

type dnsDomains struct{ s *Server }

func (p dnsDomains) Mode() string                                { return "dns" }
func (p dnsDomains) Attach(context.Context, *store.Domain) error { return nil }
func (p dnsDomains) Detach(context.Context, *store.Domain) error { return nil }
func (p dnsDomains) Release(context.Context, string, string)     {}
func (p dnsDomains) txt(d *store.Domain) (name, value string) {
	return "_zelda." + d.Hostname, "zelda-verify=" + d.VerifyToken
}
func (p dnsDomains) cname(d *store.Domain) dnsRecord {
	return dnsRecord{"CNAME", d.Hostname, p.s.cfg.DomainTarget, "routing"}
}
func (p dnsDomains) records(d *store.Domain) (txt dnsRecord, cn dnsRecord) {
	n, v := p.txt(d)
	return dnsRecord{"TXT", n, v, "ownership"}, p.cname(d)
}

func (p dnsDomains) Check(ctx context.Context, d *store.Domain) error {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	if reason := p.s.checkDomainDNS(ctx, d); reason != "" {
		return &domainError{http.StatusUnprocessableEntity, "dns_not_ready", reason}
	}
	if err := p.s.store.MarkDomainVerified(ctx, d.ID); err != nil {
		return err
	}
	d.Verified = true
	return nil
}

func (p dnsDomains) Describe(d *store.Domain, v *domainView) {
	txt, cn := p.records(d)
	v.TXTName, v.TXTValue = txt.Name, txt.Value
	v.Records = []dnsRecord{txt, cn}
	v.Status = "pending"
	if d.Verified {
		v.Status = "active"
	}
}

func (s *Server) checkDomainDNS(ctx context.Context, d *store.Domain) string {
	want := "zelda-verify=" + d.VerifyToken
	txts, err := s.resolver.LookupTXT(ctx, "_zelda."+d.Hostname)
	found := false
	for _, t := range txts {
		if strings.TrimSpace(t) == want {
			found = true
		}
	}
	if err != nil || !found {
		return "TXT record _zelda." + d.Hostname + " with value " + want + " not found yet"
	}
	got, err := s.resolver.LookupIPAddr(ctx, d.Hostname)
	if err != nil || len(got) == 0 {
		return d.Hostname + " doesn't resolve yet; add a CNAME to " + s.cfg.DomainTarget
	}
	target, err := s.resolver.LookupIPAddr(ctx, s.cfg.DomainTarget)
	if err != nil {
		return "couldn't resolve " + s.cfg.DomainTarget + "; try again shortly"
	}
	targets := map[string]bool{}
	for _, a := range target {
		targets[a.IP.String()] = true
	}
	for _, a := range got {
		if targets[a.IP.String()] {
			return ""
		}
	}
	return d.Hostname + " doesn't point at " + s.cfg.DomainTarget + " yet"
}
