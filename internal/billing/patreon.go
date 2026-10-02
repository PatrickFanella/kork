package billing

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

// PatreonWWW is Patreon's origin for OAuth and API v2.
const PatreonWWW = "https://www.patreon.com"

// PatreonScope is the OAuth scope requested. Patreon's API v2 docs: "If you
// request memberships and DON'T have the identity.memberships scope, you will
// receive data about the user's membership to your campaign." With the client
// registered under the creator account, identity alone returns the patron's
// membership to that campaign and nothing about their other pledges.
const PatreonScope = "identity"

const (
	// PatreonGrace is how long Pro stays after a check finds no qualifying
	// membership, covering a declined payment that Patreon retries.
	PatreonGrace = 7 * 24 * time.Hour
	// PatreonCheckEvery is how often memberships are re-checked.
	PatreonCheckEvery = 24 * time.Hour
)

// ErrPatreonRevoked means Patreon rejected the stored grant (the patron
// disconnected the app, or the refresh token is no longer valid).
var ErrPatreonRevoked = errors.New("patreon authorization is no longer valid")

// Patreon is an OAuth client registered on Patreon.
type Patreon struct {
	ClientID     string
	ClientSecret string
	RedirectURI  string
	BaseURL      string // PatreonWWW unless a test replaces it
	HTTP         *http.Client
}

// NewPatreon builds a client for the live API.
func NewPatreon(clientID, secret, redirectURI string) *Patreon {
	return &Patreon{ClientID: clientID, ClientSecret: secret, RedirectURI: redirectURI, BaseURL: PatreonWWW, HTTP: providerClient()}
}

// AuthorizeURL is where the browser goes to approve the connection.
func (p *Patreon) AuthorizeURL(state string) string {
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", p.ClientID)
	q.Set("redirect_uri", p.RedirectURI)
	q.Set("scope", PatreonScope)
	q.Set("state", state)
	return p.BaseURL + "/oauth2/authorize?" + q.Encode()
}

// Token is an OAuth token response.
type Token struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
}

// Exchange trades an authorization code for tokens.
func (p *Patreon) Exchange(ctx context.Context, code string) (*Token, error) {
	f := url.Values{}
	f.Set("grant_type", "authorization_code")
	f.Set("code", code)
	f.Set("redirect_uri", p.RedirectURI)
	return p.token(ctx, f)
}

// Refresh gets new tokens. Patreon rotates the refresh token, so the caller
// must store the returned one.
func (p *Patreon) Refresh(ctx context.Context, refreshToken string) (*Token, error) {
	f := url.Values{}
	f.Set("grant_type", "refresh_token")
	f.Set("refresh_token", refreshToken)
	return p.token(ctx, f)
}

func (p *Patreon) token(ctx context.Context, f url.Values) (*Token, error) {
	f.Set("client_id", p.ClientID)
	f.Set("client_secret", p.ClientSecret)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.BaseURL+"/api/oauth2/token", strings.NewReader(f.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	var t Token
	if err := p.do(req, &t); err != nil {
		return nil, err
	}
	if t.AccessToken == "" {
		return nil, errors.New("patreon: token response has no access_token")
	}
	return &t, nil
}

func (p *Patreon) do(req *http.Request, out any) error {
	res, err := p.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, maxResponse))
	if err != nil {
		return err
	}
	switch {
	case res.StatusCode == http.StatusBadRequest || res.StatusCode == http.StatusUnauthorized:
		// invalid_grant and revoked tokens; nothing to retry.
		return ErrPatreonRevoked
	case res.StatusCode < 200 || res.StatusCode > 299:
		return &APIError{Status: res.StatusCode, Message: strings.TrimSpace(string(data[:min(len(data), 200)]))}
	}
	return json.Unmarshal(data, out)
}

// IdentityQuery is the identity request: the user with their membership to
// the campaign and its entitled tiers.
const IdentityQuery = "include=memberships,memberships.currently_entitled_tiers,memberships.campaign" +
	"&fields%5Bmember%5D=patron_status,currently_entitled_amount_cents,next_charge_date&fields%5Btier%5D=title"

// Member is a patron's membership to a campaign.
type Member struct {
	ID            string
	PatronStatus  string // active_patron, declined_patron, former_patron or empty
	CampaignID    string // empty if Patreon didn't include the relationship
	TierIDs       []string
	EntitledCents int
}

// Identity is the Patreon user and their memberships.
type Identity struct {
	UserID  string
	Members []Member
}

type jsonapiRef struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

type jsonapiResource struct {
	ID            string          `json:"id"`
	Type          string          `json:"type"`
	Attributes    json.RawMessage `json:"attributes"`
	Relationships map[string]struct {
		Data json.RawMessage `json:"data"`
	} `json:"relationships"`
}

func (r jsonapiResource) refs(name string) []jsonapiRef {
	rel, ok := r.Relationships[name]
	if !ok || len(rel.Data) == 0 {
		return nil
	}
	var many []jsonapiRef
	if json.Unmarshal(rel.Data, &many) == nil {
		return many
	}
	var one jsonapiRef
	if json.Unmarshal(rel.Data, &one) == nil && one.ID != "" {
		return []jsonapiRef{one}
	}
	return nil
}

// Identity fetches /api/oauth2/v2/identity with the access token.
func (p *Patreon) Identity(ctx context.Context, accessToken string) (*Identity, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.BaseURL+"/api/oauth2/v2/identity?"+IdentityQuery, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	var doc struct {
		Data     jsonapiResource   `json:"data"`
		Included []jsonapiResource `json:"included"`
	}
	if err := p.do(req, &doc); err != nil {
		return nil, err
	}
	if doc.Data.ID == "" {
		return nil, errors.New("patreon: identity has no user id")
	}
	id := &Identity{UserID: doc.Data.ID}
	for _, r := range doc.Included {
		if r.Type != "member" {
			continue
		}
		var attrs struct {
			PatronStatus  *string `json:"patron_status"`
			EntitledCents int     `json:"currently_entitled_amount_cents"`
		}
		_ = json.Unmarshal(r.Attributes, &attrs)
		m := Member{ID: r.ID, EntitledCents: attrs.EntitledCents}
		if attrs.PatronStatus != nil {
			m.PatronStatus = *attrs.PatronStatus
		}
		for _, t := range r.refs("currently_entitled_tiers") {
			m.TierIDs = append(m.TierIDs, t.ID)
		}
		if c := r.refs("campaign"); len(c) == 1 {
			m.CampaignID = c[0].ID
		}
		id.Members = append(id.Members, m)
	}
	return id, nil
}

// Entitled decides whether the identity unlocks Pro: a membership with
// patron_status active_patron, in campaignID, currently entitled to one of
// tierIDs. Tier IDs are unique to a campaign, so a membership whose campaign
// relationship is missing still qualifies through its tier. status is the
// best patron status found in the campaign, for display.
func Entitled(id *Identity, campaignID string, tierIDs []string) (ok bool, status string) {
	for _, m := range id.Members {
		if m.CampaignID != "" && m.CampaignID != campaignID {
			continue
		}
		if status == "" || m.PatronStatus == "active_patron" {
			status = m.PatronStatus
		}
		if m.PatronStatus != "active_patron" {
			continue
		}
		for _, t := range m.TierIDs {
			if slices.Contains(tierIDs, t) {
				return true, m.PatronStatus
			}
		}
	}
	return false, status
}

// PatreonState is how long the Patreon source grants Pro. LostAt is when a
// check first found no qualifying membership; it is nil while entitled.
type PatreonState struct {
	ProUntil *time.Time
	LostAt   *time.Time
}

// PatreonNext applies a successful membership check at now:
//
//   - entitled: Pro until now + PatreonGrace + PatreonCheckEvery, so the
//     grant outlives a missed daily check but lapses if checks stop.
//   - not entitled after being entitled: Pro stays until PatreonGrace after
//     the first check that found the membership gone, then the Patreon grant
//     is dropped.
//   - never entitled: no grant.
func PatreonNext(prev PatreonState, entitled bool, now time.Time) PatreonState {
	if entitled {
		t := now.Add(PatreonGrace + PatreonCheckEvery)
		return PatreonState{ProUntil: &t}
	}
	if prev.ProUntil == nil {
		return PatreonState{}
	}
	lost := now
	if prev.LostAt != nil {
		lost = *prev.LostAt
	}
	end := lost.Add(PatreonGrace)
	if !now.Before(end) {
		return PatreonState{}
	}
	return PatreonState{ProUntil: &end, LostAt: &lost}
}
