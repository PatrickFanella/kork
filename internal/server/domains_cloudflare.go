package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"git.subcult.tv/subculture-collective/zelda/internal/cloudflare"
	"git.subcult.tv/subculture-collective/zelda/internal/store"
)

// Background refresh of pending Cloudflare domains.
const (
	cfRefreshBatch = 50
	// cfRefreshMinAge skips domains checked more recently than this, such as
	// one the user just verified by hand.
	cfRefreshMinAge = 5 * time.Minute
	// cfMissing is stored as the status when Cloudflare no longer has the hostname.
	cfMissing = "missing"
)

// cloudflareDomains registers custom domains as Cloudflare for SaaS custom
// hostnames. A domain is verified once Cloudflare reports both the hostname
// and its certificate active.
type cloudflareDomains struct {
	s   *Server
	api *cloudflare.Client
}

// UseCloudflare switches custom domains to Cloudflare for SaaS with client c.
func (s *Server) UseCloudflare(c *cloudflare.Client) { s.domains = &cloudflareDomains{s: s, api: c} }

// cfDetails is stored in domains.cf_details.
type cfDetails struct {
	Ownership  *cloudflare.Record  `json:"ownership,omitempty"`
	Validation []cloudflare.Record `json:"validation,omitempty"`
	Errors     []string            `json:"errors,omitempty"`
}

func (p *cloudflareDomains) Mode() string { return "cloudflare" }

func (p *cloudflareDomains) Attach(ctx context.Context, d *store.Domain) error {
	h, err := p.api.Create(ctx, d.Hostname)
	var ae *cloudflare.APIError
	if errors.As(err, &ae) && ae.HasCode(cloudflare.CodeDuplicate) {
		// Left over from an earlier attempt that didn't finish. The row's
		// unique hostname means no other page owns it, so adopt it.
		h, err = p.api.Find(ctx, d.Hostname)
	}
	if err != nil {
		return p.apiErr("couldn't register "+d.Hostname+" with Cloudflare", err)
	}
	if err := p.save(ctx, d, h); err != nil {
		if derr := p.api.Delete(context.WithoutCancel(ctx), h.ID); derr != nil && !errors.Is(derr, cloudflare.ErrNotFound) {
			p.s.log.Error("cloudflare: remove hostname after failed save", "hostname", d.Hostname, "id", h.ID, "err", derr)
		}
		return err
	}
	return nil
}

func (p *cloudflareDomains) Check(ctx context.Context, d *store.Domain) error {
	if d.CF.HostnameID == nil {
		// Added before Cloudflare mode was enabled.
		return p.Attach(ctx, d)
	}
	return p.refresh(ctx, d)
}

// refresh reads the hostname from Cloudflare and stores its state.
func (p *cloudflareDomains) refresh(ctx context.Context, d *store.Domain) error {
	h, err := p.api.Get(ctx, *d.CF.HostnameID)
	if errors.Is(err, cloudflare.ErrNotFound) {
		h = &cloudflare.Hostname{ID: *d.CF.HostnameID, Hostname: d.Hostname, Status: cfMissing}
	} else if err != nil {
		return p.apiErr("couldn't check "+d.Hostname+" with Cloudflare", err)
	}
	return p.save(ctx, d, h)
}

func (p *cloudflareDomains) save(ctx context.Context, d *store.Domain, h *cloudflare.Hostname) error {
	det := cfDetails{Errors: h.Errors()}
	if h.OwnershipVerification != nil && h.OwnershipVerification.Name != "" {
		o := *h.OwnershipVerification
		o.Type = strings.ToUpper(o.Type)
		det.Ownership = &o
	}
	for _, r := range h.SSL.ValidationRecords {
		if r.TXTName != "" && r.TXTValue != "" {
			det.Validation = append(det.Validation, cloudflare.Record{Type: "TXT", Name: r.TXTName, Value: r.TXTValue})
		}
		if r.CNAME != "" && r.CNAMETarget != "" {
			det.Validation = append(det.Validation, cloudflare.Record{Type: "CNAME", Name: r.CNAME, Value: r.CNAMETarget})
		}
	}
	raw, _ := json.Marshal(det)
	now := p.s.now()
	id := h.ID
	st := store.CloudflareState{HostnameID: &id, Status: h.Status, SSLStatus: h.SSL.Status, Details: raw, CheckedAt: &now}
	if err := p.s.store.SaveCloudflareState(ctx, d.ID, st, h.Active(), now); err != nil {
		return err
	}
	d.CF = st
	d.Verified = d.Verified || h.Active()
	return nil
}

func (p *cloudflareDomains) Detach(ctx context.Context, d *store.Domain) error {
	if d.CF.HostnameID == nil {
		return nil
	}
	if err := p.api.Delete(ctx, *d.CF.HostnameID); err != nil && !errors.Is(err, cloudflare.ErrNotFound) {
		return p.apiErr("couldn't remove "+d.Hostname+" from Cloudflare", err)
	}
	return nil
}

func (p *cloudflareDomains) Release(ctx context.Context, pageID, userID string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
	defer cancel()
	ids, err := p.s.store.CloudflareHostnameIDs(ctx, pageID, userID)
	if err != nil {
		p.s.log.Error("cloudflare: list hostnames to remove", "err", err)
		return
	}
	for _, id := range ids {
		if err := p.api.Delete(ctx, id); err != nil && !errors.Is(err, cloudflare.ErrNotFound) {
			p.s.log.Error("cloudflare: remove hostname; delete it in the dashboard", "id", id, "err", err)
		}
	}
}

// apiErr turns a Cloudflare failure into a message for the user. Cloudflare
// rejecting the request (4xx other than auth) is the user's to fix; anything
// else is ours and is logged.
func (p *cloudflareDomains) apiErr(what string, err error) error {
	var ae *cloudflare.APIError
	if errors.As(err, &ae) && ae.Status >= 400 && ae.Status < 500 &&
		ae.Status != http.StatusUnauthorized && ae.Status != http.StatusForbidden && ae.Status != http.StatusTooManyRequests {
		return &domainError{http.StatusUnprocessableEntity, "cloudflare_rejected", what + ": " + ae.Message()}
	}
	p.s.log.Error("cloudflare API", "what", what, "err", err)
	return &domainError{http.StatusBadGateway, "cloudflare_unavailable", what + ". Try again in a few minutes."}
}

func (p *cloudflareDomains) Describe(d *store.Domain, v *domainView) {
	var det cfDetails
	_ = json.Unmarshal(d.CF.Details, &det)
	v.Records = []dnsRecord{{"CNAME", d.Hostname, p.s.cfg.DomainTarget, "routing"}}
	if det.Ownership != nil {
		v.TXTName, v.TXTValue = det.Ownership.Name, det.Ownership.Value
		v.Records = append(v.Records, dnsRecord{det.Ownership.Type, det.Ownership.Name, det.Ownership.Value, "ownership"})
	}
	if !d.Verified {
		for _, r := range det.Validation {
			v.Records = append(v.Records, dnsRecord{r.Type, r.Name, r.Value, "certificate"})
		}
	}
	v.Errors = append(v.Errors, det.Errors...)
	v.CheckedAt = d.CF.CheckedAt
	switch {
	case d.Verified:
		v.Status = "active"
	case d.CF.HostnameID == nil:
		v.Status = "pending"
		v.Errors = append(v.Errors, "This domain isn't registered with Cloudflare yet. Press Verify to register it.")
	default:
		if msg := cfProblem(d.CF.Status, d.CF.SSLStatus); msg != "" {
			v.Status = "error"
			v.Errors = append(v.Errors, msg)
		} else {
			v.Status = "pending"
		}
	}
}

// cfProblem explains Cloudflare states that won't resolve on their own.
func cfProblem(status, ssl string) string {
	switch {
	case status == cfMissing:
		return "Cloudflare no longer has this hostname, usually because it wasn't verified in time. Remove the domain and add it again."
	case status == "blocked" || status == "pending_blocked" || status == "test_blocked":
		return "Cloudflare blocked this hostname. Contact us if you think this is a mistake."
	case status == "moved":
		return "This hostname no longer points at the CNAME target. Restore the CNAME record."
	case status == "deleted" || status == "pending_deletion" || status == "test_failed":
		return "Cloudflare is removing this hostname. Remove the domain and add it again."
	case strings.HasSuffix(ssl, "_timed_out") || ssl == "expired" || ssl == "deleted" || ssl == "inactive" || ssl == "deactivating":
		return "The certificate couldn't be issued (" + strings.ReplaceAll(ssl, "_", " ") + "). Check the CNAME record, then remove the domain and add it again."
	}
	return ""
}

// RefreshOnce rechecks up to cfRefreshBatch pending domains with Cloudflare
// and returns how many were checked and how many became verified.
func (p *cloudflareDomains) RefreshOnce(ctx context.Context) (checked, verified int, err error) {
	ds, err := p.s.store.PendingCloudflareDomains(ctx, p.s.now().Add(-cfRefreshMinAge), cfRefreshBatch)
	if err != nil {
		return 0, 0, err
	}
	var firstErr error
	for _, d := range ds {
		if ctx.Err() != nil {
			return checked, verified, ctx.Err()
		}
		if err := p.refresh(ctx, d); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		checked++
		if d.Verified {
			verified++
		}
	}
	return checked, verified, firstErr
}

// RunDomainRefresh rechecks pending Cloudflare domains every interval until
// ctx is done, so users don't have to press Verify. It returns at once in
// DNS mode.
func (s *Server) RunDomainRefresh(ctx context.Context, every time.Duration) {
	p, ok := s.domains.(*cloudflareDomains)
	if !ok || !s.cfg.CustomDomains {
		return
	}
	t := time.NewTimer(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		checked, verified, err := p.RefreshOnce(ctx)
		if err != nil && ctx.Err() == nil {
			s.log.Warn("custom domain refresh incomplete", "err", err)
		}
		if checked > 0 {
			s.log.Info("custom domain refresh", "checked", checked, "verified", verified)
		}
		t.Reset(every)
	}
}
