package server

import (
	"context"
	"strings"
	"testing"
	"time"

	"git.subcult.tv/subculture-collective/zelda/internal/cloudflare/cftest"
	"git.subcult.tv/subculture-collective/zelda/internal/plans"
)

type cfDomain struct {
	ID       string
	Hostname string
	Verified bool
	Active   bool
	Mode     string
	Status   string
	TXTName  string `json:"txt_name"`
	TXTValue string `json:"txt_value"`
	Records  []dnsRecord
	Errors   []string
}

func (d cfDomain) record(purpose string) *dnsRecord {
	for i := range d.Records {
		if d.Records[i].Purpose == purpose {
			return &d.Records[i]
		}
	}
	return nil
}

func TestCloudflareDomains(t *testing.T) {
	e := setup(t)
	api := cftest.New(t)
	api.OwnershipTXT = true
	e.srv.UseCloudflare(api.Client())
	clock := &testClock{now: time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)}
	e.srv.SetClock(clock.Now)
	cf := e.srv.domains.(*cloudflareDomains)
	anon := e.client()
	bob := e.client()
	bob.signupVerified("bob@example.com")
	pageID := bob.newPage("bobpage")
	if code := bob.do("PATCH", "/api/pages/"+pageID, map[string]any{"published": true}, nil); code != 200 {
		t.Fatalf("publish: %d", code)
	}
	domainsOf := func() []cfDomain {
		var p struct{ Domains []cfDomain }
		if code := bob.do("GET", "/api/pages/"+pageID, nil, &p); code != 200 {
			t.Fatalf("get page: %d", code)
		}
		return p.Domains
	}

	// The catalog tells the editor which instructions to show.
	var cat struct {
		DomainMode   string `json:"domain_mode"`
		DomainTarget string `json:"domain_target"`
	}
	anon.do("GET", "/api/catalog", nil, &cat)
	if cat.DomainMode != "cloudflare" || cat.DomainTarget != "edge.example.net" {
		t.Fatalf("catalog: %+v", cat)
	}

	// ---- The Pro gate applies before Cloudflare is called ----
	var ue upgradeErr
	if code := bob.do("POST", "/api/pages/"+pageID+"/domains", map[string]string{"hostname": "links.bob.example"}, &ue); code != 403 ||
		ue.Error.Code != "upgrade_required" || ue.Error.Feature != "custom_domains" {
		t.Fatalf("free domain: %d %+v", code, ue)
	}
	if n := api.Count("POST"); n != 0 {
		t.Fatalf("free account reached Cloudflare: %d calls", n)
	}
	bob.setPlan(plans.Pro, nil)

	// ---- Create failures leave no domain behind ----
	api.FailCreate, api.FailCreateMsg = 400, "Hostname is not allowed."
	var ae apiErr
	if code := bob.do("POST", "/api/pages/"+pageID+"/domains", map[string]string{"hostname": "links.bob.example"}, &ae); code != 422 ||
		ae.Error.Code != "cloudflare_rejected" || !strings.Contains(ae.Error.Message, "Hostname is not allowed.") {
		t.Fatalf("rejected create: %d %+v", code, ae)
	}
	api.FailCreate = 503
	if code := bob.do("POST", "/api/pages/"+pageID+"/domains", map[string]string{"hostname": "links.bob.example"}, &ae); code != 502 ||
		ae.Error.Code != "cloudflare_unavailable" {
		t.Fatalf("unavailable create: %d %+v", code, ae)
	}
	if ds := domainsOf(); len(ds) != 0 {
		t.Fatalf("half-created domain kept: %+v", ds)
	}
	api.FailCreate = 0

	// ---- Create → pending: show the CNAME target and ownership TXT ----
	var d cfDomain
	if code := bob.do("POST", "/api/pages/"+pageID+"/domains", map[string]string{"hostname": "links.bob.example"}, &d); code != 201 {
		t.Fatalf("create: %d %+v", code, d)
	}
	if !api.Has("links.bob.example") || d.Verified || d.Mode != "cloudflare" || d.Status != "pending" {
		t.Fatalf("created: %+v", d)
	}
	if r := d.record("routing"); r == nil || r.Type != "CNAME" || r.Name != "links.bob.example" || r.Value != "edge.example.net" {
		t.Fatalf("routing record: %+v", d.Records)
	}
	if r := d.record("ownership"); r == nil || r.Type != "TXT" || r.Name != "_cf-custom-hostname.links.bob.example" || r.Value == "" ||
		d.TXTName != r.Name || d.TXTValue != r.Value {
		t.Fatalf("ownership record: %+v", d)
	}
	if len(d.Errors) != 1 || !strings.Contains(d.Errors[0], "does not CNAME") {
		t.Fatalf("errors: %v", d.Errors)
	}

	// Verify while Cloudflare is still issuing the certificate: 200, not verified.
	api.Set("links.bob.example", "active", "pending_validation")
	if code := bob.do("POST", "/api/pages/"+pageID+"/domains/"+d.ID+"/verify", nil, &d); code != 200 || d.Verified || d.Status != "pending" || len(d.Errors) != 0 {
		t.Fatalf("verify pending: %d %+v", code, d)
	}
	if code, _, _ := anon.get("/", "Host", "links.bob.example"); code != 404 {
		t.Fatalf("unverified domain served: %d", code)
	}

	// ---- The background refresh marks it verified once both are active ----
	api.Set("links.bob.example", "active", "active")
	if checked, _, err := cf.RefreshOnce(context.Background()); err != nil || checked != 0 {
		t.Fatalf("refresh skips a domain checked just now: %d %v", checked, err)
	}
	clock.Set(clock.Now().Add(cfRefreshMinAge + time.Second))
	if checked, verified, err := cf.RefreshOnce(context.Background()); err != nil || checked != 1 || verified != 1 {
		t.Fatalf("refresh: %d %d %v", checked, verified, err)
	}
	if ds := domainsOf(); len(ds) != 1 || !ds[0].Verified || ds[0].Status != "active" || !ds[0].Active {
		t.Fatalf("after refresh: %+v", ds)
	}
	if code, _, _ := anon.get("/", "Host", "links.bob.example", "User-Agent", "Mozilla/5.0"); code != 200 {
		t.Fatalf("verified domain not served: %d", code)
	}
	clock.Set(clock.Now().Add(time.Hour))
	if checked, _, _ := cf.RefreshOnce(context.Background()); checked != 0 {
		t.Fatalf("verified domains aren't refreshed: %d", checked)
	}

	// ---- Delete removes the custom hostname; a failed delete keeps the row ----
	api.FailDelete = 500
	if code := bob.do("DELETE", "/api/pages/"+pageID+"/domains/"+d.ID, nil, &ae); code != 502 || ae.Error.Code != "cloudflare_unavailable" {
		t.Fatalf("failed delete: %d %+v", code, ae)
	}
	if len(domainsOf()) != 1 {
		t.Fatal("row removed although Cloudflare still has the hostname")
	}
	api.FailDelete = 0
	if code := bob.do("DELETE", "/api/pages/"+pageID+"/domains/"+d.ID, nil, nil); code != 204 || api.Has("links.bob.example") {
		t.Fatalf("delete: %d", code)
	}

	// Deleting tolerates a hostname Cloudflare already dropped.
	bob.do("POST", "/api/pages/"+pageID+"/domains", map[string]string{"hostname": "links.bob.example"}, &d)
	api.Remove("links.bob.example")
	if code := bob.do("DELETE", "/api/pages/"+pageID+"/domains/"+d.ID, nil, nil); code != 204 {
		t.Fatalf("delete after 404: %d", code)
	}

	// ---- A hostname Cloudflare dropped while pending shows as an error ----
	bob.do("POST", "/api/pages/"+pageID+"/domains", map[string]string{"hostname": "links.bob.example"}, &d)
	api.Remove("links.bob.example")
	clock.Set(clock.Now().Add(cfRefreshMinAge + time.Second))
	if checked, _, err := cf.RefreshOnce(context.Background()); err != nil || checked != 1 {
		t.Fatalf("refresh missing: %d %v", checked, err)
	}
	if ds := domainsOf(); len(ds) != 1 || ds[0].Status != "error" || !strings.Contains(strings.Join(ds[0].Errors, " "), "no longer has this hostname") {
		t.Fatalf("missing hostname: %+v", ds)
	}
	clock.Set(clock.Now().Add(time.Hour))
	if checked, _, _ := cf.RefreshOnce(context.Background()); checked != 0 {
		t.Fatalf("missing hostnames aren't refreshed: %d", checked)
	}
	bob.do("DELETE", "/api/pages/"+pageID+"/domains/"+d.ID, nil, nil)

	// ---- A hostname left in Cloudflare by an earlier attempt is adopted ----
	left, err := api.Client().Create(context.Background(), "links.bob.example")
	if err != nil {
		t.Fatal(err)
	}
	if code := bob.do("POST", "/api/pages/"+pageID+"/domains", map[string]string{"hostname": "links.bob.example"}, &d); code != 201 {
		t.Fatalf("adopt: %d", code)
	}
	var ids []string
	if ids, err = e.srv.store.CloudflareHostnameIDs(context.Background(), pageID, ""); err != nil || len(ids) != 1 || ids[0] != left.ID {
		t.Fatalf("adopted id: %v %v (want %s)", ids, err, left.ID)
	}

	// ---- Verifying still needs Pro ----
	bob.setPlan(plans.Free, nil)
	if code := bob.do("POST", "/api/pages/"+pageID+"/domains/"+d.ID+"/verify", nil, &ue); code != 403 || ue.Error.Code != "upgrade_required" {
		t.Fatalf("free verify: %d %+v", code, ue)
	}

	// ---- Deleting the page removes its custom hostnames ----
	if code := bob.do("DELETE", "/api/pages/"+pageID, nil, nil); code != 204 || api.Has("links.bob.example") {
		t.Fatalf("delete page: %d, hostname kept: %v", code, api.Has("links.bob.example"))
	}

	// The refresh loop stops with its context.
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { e.srv.RunDomainRefresh(ctx, time.Minute); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("refresh loop ignored cancellation")
	}
}

// A domain added in DNS mode is registered with Cloudflare on its next Verify.
func TestCloudflareAdoptsDNSModeDomain(t *testing.T) {
	e := setup(t)
	bob := e.client()
	bob.signupVerified("bob@example.com")
	bob.setPlan(plans.Pro, nil)
	pageID := bob.newPage("bobpage")
	var d cfDomain
	if code := bob.do("POST", "/api/pages/"+pageID+"/domains", map[string]string{"hostname": "links.bob.example"}, &d); code != 201 ||
		d.Mode != "dns" || d.Status != "pending" || len(d.Records) != 2 || d.record("ownership").Name != "_zelda.links.bob.example" {
		t.Fatalf("dns mode create: %d %+v", code, d)
	}
	api := cftest.New(t)
	e.srv.UseCloudflare(api.Client())
	var p struct{ Domains []cfDomain }
	bob.do("GET", "/api/pages/"+pageID, nil, &p)
	if len(p.Domains) != 1 || p.Domains[0].Status != "pending" || len(p.Domains[0].Errors) != 1 {
		t.Fatalf("unregistered: %+v", p.Domains)
	}
	if code := bob.do("POST", "/api/pages/"+pageID+"/domains/"+d.ID+"/verify", nil, &d); code != 200 || !api.Has("links.bob.example") || d.Verified {
		t.Fatalf("register on verify: %d %+v", code, d)
	}
	// Deleting the account removes the custom hostname too.
	if code := bob.do("DELETE", "/api/account", map[string]string{"password": "correct horse battery"}, nil); code != 204 || api.Has("links.bob.example") {
		t.Fatalf("delete account: %d, hostname kept: %v", code, api.Has("links.bob.example"))
	}
}
