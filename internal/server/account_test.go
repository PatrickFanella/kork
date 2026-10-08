package server

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git.subcult.tv/subculture-collective/zelda/internal/plans"
	"git.subcult.tv/subculture-collective/zelda/internal/render"
	"git.subcult.tv/subculture-collective/zelda/internal/store"
)

type accountResp struct {
	User            store.User `json:"user"`
	PendingEmail    *string    `json:"pending_email"`
	TermsVersion    *string    `json:"terms_version"`
	TermsAcceptedAt *string    `json:"terms_accepted_at"`
}

func signup(t *testing.T, c *client, email, pw string) {
	t.Helper()
	if code := c.do("POST", "/api/auth/signup", map[string]any{"email": email, "password": pw, "accept_terms": true}, nil); code != 201 {
		t.Fatalf("signup %s: %d", email, code)
	}
}

func TestLegalPagesAndTermsAcceptance(t *testing.T) {
	e := setup(t)
	e.srv.cfg.ContactEmail = "legal@example.org"
	anon := e.client()

	for _, path := range []string{"/terms", "/privacy"} {
		code, body, _ := anon.get(path)
		if code != 200 {
			t.Fatalf("%s: %d", path, code)
		}
		for _, want := range []string{"legal@example.org", render.TermsVersion} {
			if !strings.Contains(body, want) {
				t.Errorf("%s missing %q", path, want)
			}
		}
		if strings.Contains(body, "DRAFT FOR OWNER REVIEW") || strings.Contains(body, "To be added.") {
			t.Errorf("%s still contains draft placeholders", path)
		}
	}
	if _, body, _ := anon.get("/terms"); !strings.Contains(body, "at least 18 years old") || !strings.Contains(body, "State of Illinois") {
		t.Error("terms missing approved age or governing law")
	}
	if _, body, _ := anon.get("/privacy"); !strings.Contains(body, "14 nightly copies") || !strings.Contains(body, "argon2id") {
		t.Error("privacy policy missing backup retention or password hashing")
	}
	if _, body, _ := anon.get("/"); !strings.Contains(body, `href="/terms"`) || !strings.Contains(body, `href="/privacy"`) {
		t.Error("landing page should link terms and privacy")
	}

	// Signup requires agreeing to the terms and records the version.
	alice := e.client()
	var ae apiErr
	if code := alice.do("POST", "/api/auth/signup", map[string]any{"email": "alice@example.com", "password": "correct horse battery"}, &ae); code != 422 || ae.Error.Field != "accept_terms" {
		t.Fatalf("signup without terms: %d %+v", code, ae)
	}
	signup(t, alice, "alice@example.com", "correct horse battery")
	var acct accountResp
	if code := alice.do("GET", "/api/account", nil, &acct); code != 200 || acct.TermsVersion == nil || *acct.TermsVersion != render.TermsVersion || acct.TermsAcceptedAt == nil {
		t.Fatalf("account terms: %d %+v", code, acct)
	}

	// Accounts from before the terms existed still work without accepting.
	hash, _ := hashPassword("an older password")
	if _, err := e.srv.store.CreateUser(context.Background(), "old@example.com", hash); err != nil {
		t.Fatal(err)
	}
	old := e.client()
	if code := old.do("POST", "/api/auth/login", map[string]string{"email": "old@example.com", "password": "an older password"}, nil); code != 200 {
		t.Fatalf("old login: %d", code)
	}
	acct = accountResp{}
	if code := old.do("GET", "/api/account", nil, &acct); code != 200 || acct.TermsVersion != nil {
		t.Fatalf("old account: %d %+v", code, acct)
	}

	// Public pages link the terms on the main host.
	if code := alice.do("POST", "/api/pages", map[string]string{"slug": "alice", "title": "Alice"}, nil); code != 201 {
		t.Fatal("create page")
	}
	if _, body, _ := alice.get("/preview/" + firstPageID(t, alice)); !strings.Contains(body, e.ts.URL+"/terms") {
		t.Error("page footer should link the terms")
	}
}

func firstPageID(t *testing.T, c *client) string {
	t.Helper()
	var pages []struct{ ID string }
	c.do("GET", "/api/pages", nil, &pages)
	if len(pages) == 0 {
		t.Fatal("no pages")
	}
	return pages[0].ID
}

func TestChangePassword(t *testing.T) {
	e := setup(t)
	a1, a2 := e.client(), e.client()
	signup(t, a1, "alice@example.com", "correct horse battery")
	a2.do("POST", "/api/auth/login", map[string]string{"email": "alice@example.com", "password": "correct horse battery"}, nil)

	var ae apiErr
	if code := a1.do("POST", "/api/account/password", map[string]string{"current_password": "wrong password!", "new_password": "a brand new password"}, &ae); code != 422 || ae.Error.Field != "current_password" {
		t.Fatalf("wrong current: %d %+v", code, ae)
	}
	if code := a1.do("POST", "/api/account/password", map[string]string{"current_password": "correct horse battery", "new_password": "short"}, &ae); code != 422 || ae.Error.Field != "new_password" {
		t.Fatalf("weak new: %d %+v", code, ae)
	}
	if code := a1.do("POST", "/api/account/password", map[string]string{"current_password": "correct horse battery", "new_password": "a brand new password"}, nil); code != 204 {
		t.Fatalf("change: %d", code)
	}
	if code := a1.do("GET", "/api/auth/me", nil, nil); code != 200 {
		t.Fatalf("current session should survive: %d", code)
	}
	if code := a2.do("GET", "/api/auth/me", nil, nil); code != 401 {
		t.Fatalf("other session should be revoked: %d", code)
	}
	if code := e.client().do("POST", "/api/auth/login", map[string]string{"email": "alice@example.com", "password": "correct horse battery"}, nil); code != 401 {
		t.Fatalf("old password still works: %d", code)
	}
	if code := e.client().do("POST", "/api/auth/login", map[string]string{"email": "alice@example.com", "password": "a brand new password"}, nil); code != 200 {
		t.Fatalf("new password: %d", code)
	}
}

func TestChangeEmail(t *testing.T) {
	e := setup(t)
	alice := e.client()
	signup(t, alice, "alice@example.com", "correct horse battery")
	signup(t, e.client(), "bob@example.com", "correct horse battery")

	var ae apiErr
	if code := alice.do("POST", "/api/account/email", map[string]string{"email": "new@example.com", "password": "nope nope nope"}, &ae); code != 422 || ae.Error.Code != "wrong_password" {
		t.Fatalf("wrong password: %d %+v", code, ae)
	}
	if code := alice.do("POST", "/api/account/email", map[string]string{"email": "BOB@example.com", "password": "correct horse battery"}, &ae); code != 409 {
		t.Fatalf("taken email: %d %+v", code, ae)
	}
	var acct accountResp
	if code := alice.do("POST", "/api/account/email", map[string]string{"email": "new@example.com", "password": "correct horse battery"}, &acct); code != 202 || acct.PendingEmail == nil || *acct.PendingEmail != "new@example.com" {
		t.Fatalf("request change: %d %+v", code, acct)
	}
	// The old address stays in use until the new one is confirmed.
	var me struct{ User store.User }
	alice.do("GET", "/api/auth/me", nil, &me)
	if me.User.Email != "alice@example.com" {
		t.Fatalf("email switched before confirmation: %s", me.User.Email)
	}
	tok := e.mail.tokenFor(t, "new@example.com", "/app/confirm-email")
	if code := e.client().do("POST", "/api/account/email/confirm", map[string]string{"token": "bogus"}, &ae); code != 400 {
		t.Fatalf("bogus token: %d", code)
	}
	if code := e.client().do("POST", "/api/account/email/confirm", map[string]string{"token": tok}, &me); code != 200 || me.User.Email != "new@example.com" || !me.User.EmailVerified {
		t.Fatalf("confirm: %d %+v", code, me.User)
	}
	if code := e.client().do("POST", "/api/account/email/confirm", map[string]string{"token": tok}, &ae); code != 400 {
		t.Fatalf("token reuse: %d", code)
	}
	acct = accountResp{}
	alice.do("GET", "/api/account", nil, &acct)
	if acct.User.Email != "new@example.com" || acct.PendingEmail != nil {
		t.Fatalf("after confirm: %+v", acct)
	}
	if code := e.client().do("POST", "/api/auth/login", map[string]string{"email": "alice@example.com", "password": "correct horse battery"}, nil); code != 401 {
		t.Fatalf("old email still logs in: %d", code)
	}
	if code := e.client().do("POST", "/api/auth/login", map[string]string{"email": "new@example.com", "password": "correct horse battery"}, nil); code != 200 {
		t.Fatalf("new email login: %d", code)
	}
}

// buildAccount gives alice a published page with blocks, an avatar upload,
// a domain and one recorded view. It returns the page and upload IDs.
func buildAccount(t *testing.T, e *env, alice *client) (string, string) {
	t.Helper()
	signup(t, alice, "alice@example.com", "correct horse battery")
	tok := e.mail.tokenFor(t, "alice@example.com", "/app/verify")
	alice.do("POST", "/api/auth/verify", map[string]string{"token": tok}, nil)
	var page pageResp
	if code := alice.do("POST", "/api/pages", map[string]string{"slug": "alice", "title": "Alice"}, &page); code != 201 {
		t.Fatalf("create page: %d", code)
	}
	for _, b := range []map[string]any{
		{"type": "service", "data": map[string]any{"service": "instagram", "value": "@alice"}},
		{"type": "heading", "data": map[string]any{"text": "Hello"}},
	} {
		if code := alice.do("POST", "/api/pages/"+page.ID+"/blocks", b, nil); code != 201 {
			t.Fatalf("create block: %d", code)
		}
	}
	upID := uploadPNG(t, e, alice)
	if code := alice.do("PATCH", "/api/pages/"+page.ID, map[string]any{"avatar_upload_id": upID, "published": true,
		"theme": map[string]any{"preset": "paper"}}, nil); code != 200 {
		t.Fatalf("update page: %d", code)
	}
	alice.setPlan(plans.Pro, nil) // custom domains need Pro
	if code := alice.do("POST", "/api/pages/"+page.ID+"/domains", map[string]string{"hostname": "links.alice.example"}, nil); code != 201 {
		t.Fatalf("add domain: %d", code)
	}
	if code, _, _ := e.client().get("/alice", "User-Agent", "Mozilla/5.0"); code != 200 {
		t.Fatalf("public page: %d", code)
	}
	return page.ID, upID
}

func TestExport(t *testing.T) {
	e := setup(t)
	alice := e.client()
	pageID, upID := buildAccount(t, e, alice)

	if code, _, _ := e.client().get("/api/account/export"); code != 401 {
		t.Fatalf("anonymous export: %d", code)
	}
	code, body, hdr := alice.get("/api/account/export")
	if code != 200 || hdr.Get("Content-Type") != "application/zip" || !strings.Contains(hdr.Get("Content-Disposition"), "attachment") {
		t.Fatalf("export: %d %v", code, hdr)
	}
	zr, err := zip.NewReader(bytes.NewReader([]byte(body)), int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		files[f.Name], _ = io.ReadAll(rc)
		rc.Close()
	}
	var acct struct {
		Email     string `json:"email"`
		CreatedAt string `json:"created_at"`
		Terms     string `json:"terms_version"`
		Pages     []struct {
			ID     string `json:"id"`
			Slug   string `json:"slug"`
			Avatar string `json:"avatar_upload_id"`
			Theme  struct {
				Preset string `json:"preset"`
			} `json:"theme"`
			Blocks []struct {
				Type string         `json:"type"`
				Data map[string]any `json:"data"`
			} `json:"blocks"`
			Domains []struct {
				Hostname string `json:"hostname"`
			} `json:"domains"`
			Stats []store.StatRow `json:"stats"`
		} `json:"pages"`
		Uploads []struct {
			ID   string `json:"id"`
			File string `json:"file"`
		} `json:"uploads"`
	}
	if err := json.Unmarshal(files["account.json"], &acct); err != nil {
		t.Fatalf("account.json: %v (%d files)", err, len(files))
	}
	if acct.Email != "alice@example.com" || acct.CreatedAt == "" || acct.Terms != render.TermsVersion || len(acct.Pages) != 1 {
		t.Fatalf("account: %+v", acct)
	}
	p := acct.Pages[0]
	if p.ID != pageID || p.Slug != "alice" || p.Avatar != upID || p.Theme.Preset != "paper" ||
		len(p.Blocks) != 2 || p.Blocks[0].Data["value"] != "alice" || len(p.Domains) != 1 ||
		p.Domains[0].Hostname != "links.alice.example" || len(p.Stats) != 1 || p.Stats[0].Count != 1 {
		t.Fatalf("page: %+v", p)
	}
	if len(acct.Uploads) != 1 || acct.Uploads[0].ID != upID || acct.Uploads[0].File != "uploads/"+upID+".png" {
		t.Fatalf("uploads: %+v", acct.Uploads)
	}
	onDisk, _ := os.ReadFile(filepath.Join(e.dir, upID))
	if got := files[acct.Uploads[0].File]; len(onDisk) == 0 || !bytes.Equal(got, onDisk) {
		t.Fatalf("upload bytes differ: %d vs %d", len(got), len(onDisk))
	}
	if strings.Contains(string(files["account.json"]), "password") || strings.Contains(string(files["account.json"]), "verify_token") {
		t.Error("export leaks secrets")
	}

	// Another account's export doesn't include Alice's data.
	bob := e.client()
	signup(t, bob, "bob@example.com", "correct horse battery")
	if _, body, _ := bob.get("/api/account/export"); strings.Contains(body, "alice") {
		t.Error("bob's export contains alice's data")
	}

	// Rate limited: five per hour.
	for i := 0; i < 4; i++ {
		alice.get("/api/account/export")
	}
	if code, _, _ := alice.get("/api/account/export"); code != 429 {
		t.Fatalf("export rate limit: %d", code)
	}
}

func TestDeleteAccount(t *testing.T) {
	e := setup(t)
	alice := e.client()
	pageID, upID := buildAccount(t, e, alice)
	other := e.client()
	other.do("POST", "/api/auth/login", map[string]string{"email": "alice@example.com", "password": "correct horse battery"}, nil)
	ctx := context.Background()

	var ae apiErr
	if code := alice.do("DELETE", "/api/account", map[string]string{"password": "not my password"}, &ae); code != 422 {
		t.Fatalf("delete with wrong password: %d", code)
	}
	if code, _, _ := e.client().get("/alice"); code != 200 {
		t.Fatalf("page gone after failed delete: %d", code)
	}
	if code := alice.do("DELETE", "/api/account", map[string]string{"password": "correct horse battery"}, nil); code != 204 {
		t.Fatalf("delete: %d", code)
	}

	if code, _, _ := e.client().get("/alice"); code != 404 {
		t.Fatalf("deleted account's page: %d", code)
	}
	if code, _, _ := e.client().get("/u/" + upID); code != 404 {
		t.Fatalf("deleted upload served: %d", code)
	}
	if _, err := os.Stat(filepath.Join(e.dir, upID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("upload file still on disk: %v", err)
	}
	if code := alice.do("GET", "/api/auth/me", nil, nil); code != 401 {
		t.Fatalf("session after delete: %d", code)
	}
	if code := other.do("GET", "/api/auth/me", nil, nil); code != 401 {
		t.Fatalf("other session after delete: %d", code)
	}
	if code := e.client().do("POST", "/api/auth/login", map[string]string{"email": "alice@example.com", "password": "correct horse battery"}, nil); code != 401 {
		t.Fatalf("login after delete: %d", code)
	}
	if _, err := e.srv.store.UserByEmail(ctx, "alice@example.com"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("user row: %v", err)
	}
	if _, err := e.srv.store.PageByID(ctx, pageID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("page row: %v", err)
	}
	if rows, err := e.srv.store.StatRows(ctx, pageID); err != nil || len(rows) != 0 {
		t.Fatalf("stats rows: %v %v", rows, err)
	}
	// The name is free again.
	bob := e.client()
	signup(t, bob, "alice@example.com", "correct horse battery")
	var page pageResp
	if code := bob.do("POST", "/api/pages", map[string]string{"slug": "alice"}, &page); code != 201 {
		t.Fatalf("slug not freed: %d", code)
	}
	// Hostnames are unique, so this only succeeds if the old domain row is gone.
	bob.setPlan(plans.Pro, nil)
	if code := bob.do("POST", "/api/pages/"+page.ID+"/domains", map[string]string{"hostname": "links.alice.example"}, nil); code != 201 {
		t.Fatalf("domain row survived: %d", code)
	}
}
