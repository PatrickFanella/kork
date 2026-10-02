package billing

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

const identityDoc = `{
  "data": {"id": "pu_1", "type": "user", "relationships": {"memberships": {"data": [{"id": "m_other", "type": "member"}, {"id": "m_1", "type": "member"}]}}},
  "included": [
    {"id": "m_other", "type": "member", "attributes": {"patron_status": "active_patron", "currently_entitled_amount_cents": 300},
     "relationships": {"campaign": {"data": {"id": "c_other", "type": "campaign"}}, "currently_entitled_tiers": {"data": [{"id": "t_backer", "type": "tier"}]}}},
    {"id": "m_1", "type": "member", "attributes": {"patron_status": "active_patron", "currently_entitled_amount_cents": 1000, "next_charge_date": "2026-11-01T00:00:00.000+00:00"},
     "relationships": {"campaign": {"data": {"id": "c_subcult", "type": "campaign"}}, "currently_entitled_tiers": {"data": [{"id": "t_backer", "type": "tier"}]}}},
    {"id": "t_backer", "type": "tier", "attributes": {"title": "Backer"}}
  ]
}`

func TestPatreonClient(t *testing.T) {
	var form url.Values
	var query string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/oauth2/token":
			r.ParseForm()
			form = r.PostForm
			if form.Get("code") == "bad" || form.Get("refresh_token") == "revoked" {
				w.WriteHeader(400)
				w.Write([]byte(`{"error":"invalid_grant"}`))
				return
			}
			w.Write([]byte(`{"access_token":"at_1","refresh_token":"rt_2","expires_in":2678400,"scope":"identity","token_type":"Bearer"}`))
		case "/api/oauth2/v2/identity":
			query = r.URL.RawQuery
			if r.Header.Get("Authorization") != "Bearer at_1" {
				w.WriteHeader(401)
				return
			}
			w.Write([]byte(identityDoc))
		default:
			w.WriteHeader(500)
		}
	}))
	defer fake.Close()
	p := &Patreon{ClientID: "cid", ClientSecret: "csecret", RedirectURI: "https://kork.example/app/patreon/callback", BaseURL: fake.URL, HTTP: providerClient()}
	ctx := context.Background()

	au, _ := url.Parse(p.AuthorizeURL("st"))
	if au.Path != "/oauth2/authorize" || au.Query().Get("scope") != "identity" || au.Query().Get("state") != "st" ||
		au.Query().Get("redirect_uri") != p.RedirectURI || au.Query().Get("response_type") != "code" {
		t.Fatalf("authorize url: %s", au)
	}
	tok, err := p.Exchange(ctx, "code1")
	if err != nil || tok.AccessToken != "at_1" || tok.RefreshToken != "rt_2" {
		t.Fatalf("exchange: %+v %v", tok, err)
	}
	if form.Get("grant_type") != "authorization_code" || form.Get("client_secret") != "csecret" || form.Get("redirect_uri") != p.RedirectURI {
		t.Fatalf("exchange form: %v", form)
	}
	if _, err := p.Exchange(ctx, "bad"); !errors.Is(err, ErrPatreonRevoked) {
		t.Fatalf("bad code: %v", err)
	}
	if _, err := p.Refresh(ctx, "rt_1"); err != nil || form.Get("grant_type") != "refresh_token" || form.Get("refresh_token") != "rt_1" {
		t.Fatalf("refresh: %v %v", err, form)
	}
	if _, err := p.Refresh(ctx, "revoked"); !errors.Is(err, ErrPatreonRevoked) {
		t.Fatalf("revoked refresh: %v", err)
	}
	id, err := p.Identity(ctx, "at_1")
	if err != nil || id.UserID != "pu_1" || len(id.Members) != 2 {
		t.Fatalf("identity: %+v %v", id, err)
	}
	if q, _ := url.ParseQuery(query); q.Get("include") != "memberships,memberships.currently_entitled_tiers,memberships.campaign" ||
		q.Get("fields[member]") != "patron_status,currently_entitled_amount_cents,next_charge_date" || q.Get("fields[tier]") != "title" {
		t.Fatalf("identity query: %s", query)
	}
	m := id.Members[1]
	if m.CampaignID != "c_subcult" || m.PatronStatus != "active_patron" || len(m.TierIDs) != 1 || m.TierIDs[0] != "t_backer" || m.EntitledCents != 1000 {
		t.Fatalf("member: %+v", m)
	}
	if _, err := p.Identity(ctx, "expired"); !errors.Is(err, ErrPatreonRevoked) {
		t.Fatalf("expired access token: %v", err)
	}
}

func TestEntitled(t *testing.T) {
	tiers := []string{"t_backer", "t_super"}
	mk := func(ms ...Member) *Identity { return &Identity{UserID: "u", Members: ms} }
	cases := []struct {
		name   string
		id     *Identity
		ok     bool
		status string
	}{
		{"no memberships", mk(), false, ""},
		{"active backer", mk(Member{PatronStatus: "active_patron", CampaignID: "c", TierIDs: []string{"t_backer"}}), true, "active_patron"},
		{"second listed tier", mk(Member{PatronStatus: "active_patron", CampaignID: "c", TierIDs: []string{"t_free", "t_super"}}), true, "active_patron"},
		{"campaign missing, tier matches", mk(Member{PatronStatus: "active_patron", TierIDs: []string{"t_backer"}}), true, "active_patron"},
		{"other campaign", mk(Member{PatronStatus: "active_patron", CampaignID: "x", TierIDs: []string{"t_backer"}}), false, ""},
		{"lower tier", mk(Member{PatronStatus: "active_patron", CampaignID: "c", TierIDs: []string{"t_free"}}), false, "active_patron"},
		{"declined", mk(Member{PatronStatus: "declined_patron", CampaignID: "c", TierIDs: []string{"t_backer"}}), false, "declined_patron"},
		{"former", mk(Member{PatronStatus: "former_patron", CampaignID: "c"}), false, "former_patron"},
		{"follower", mk(Member{CampaignID: "c"}), false, ""},
	}
	for _, c := range cases {
		ok, status := Entitled(c.id, "c", tiers)
		if ok != c.ok || status != c.status {
			t.Errorf("%s: got %v %q", c.name, ok, status)
		}
	}
}

func TestPatreonNextGrace(t *testing.T) {
	day := 24 * time.Hour
	t0 := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)

	s := PatreonNext(PatreonState{}, false, t0)
	if s.ProUntil != nil {
		t.Fatal("never entitled must not grant")
	}
	s = PatreonNext(s, true, t0)
	if !s.ProUntil.Equal(t0.Add(PatreonGrace+PatreonCheckEvery)) || s.LostAt != nil {
		t.Fatalf("entitled: %+v", s)
	}
	// Membership lost on day 1: grace runs 7 days from that check.
	lost := t0.Add(day)
	s = PatreonNext(s, false, lost)
	if s.LostAt == nil || !s.LostAt.Equal(lost) || !s.ProUntil.Equal(lost.Add(PatreonGrace)) {
		t.Fatalf("lost: %+v", s)
	}
	// Later failing checks keep the original grace end.
	s = PatreonNext(s, false, lost.Add(3*day))
	if !s.LostAt.Equal(lost) || !s.ProUntil.Equal(lost.Add(PatreonGrace)) {
		t.Fatalf("still lost: %+v", s)
	}
	// Payment goes through again within grace: restored.
	back := PatreonNext(s, true, lost.Add(4*day))
	if back.LostAt != nil || !back.ProUntil.Equal(lost.Add(4*day+PatreonGrace+PatreonCheckEvery)) {
		t.Fatalf("restored: %+v", back)
	}
	// Grace runs out: the grant is dropped.
	s = PatreonNext(s, false, lost.Add(PatreonGrace))
	if s.ProUntil != nil || s.LostAt != nil {
		t.Fatalf("after grace: %+v", s)
	}
	// Grace counts from the check that saw the loss, even when the last
	// successful check was more than a day earlier.
	short := t0.Add(2 * day)
	s = PatreonNext(PatreonState{ProUntil: &short}, false, t0.Add(day))
	if !s.ProUntil.Equal(t0.Add(day + PatreonGrace)) {
		t.Fatalf("grace from detection: %+v", s)
	}
}
