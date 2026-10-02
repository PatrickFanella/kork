package cloudflare_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"git.subcult.tv/subculture-collective/zelda/internal/cloudflare"
	"git.subcult.tv/subculture-collective/zelda/internal/cloudflare/cftest"
)

func TestLifecycle(t *testing.T) {
	api := cftest.New(t)
	api.OwnershipTXT = true
	api.ValidationTXTs = true
	c := api.Client()
	ctx := context.Background()

	h, err := c.Create(ctx, "links.alice.example")
	if err != nil {
		t.Fatal(err)
	}
	if h.ID == "" || h.Status != "pending" || h.SSL.Status != "pending_validation" || h.Active() {
		t.Fatalf("created: %+v", h)
	}
	if h.OwnershipVerification == nil || h.OwnershipVerification.Name != "_cf-custom-hostname.links.alice.example" {
		t.Fatalf("ownership: %+v", h.OwnershipVerification)
	}
	if len(h.SSL.ValidationRecords) != 2 || h.SSL.ValidationRecords[0].TXTName != "_acme-challenge.links.alice.example" {
		t.Fatalf("validation records: %+v", h.SSL.ValidationRecords)
	}
	if errs := h.Errors(); len(errs) != 1 || !strings.Contains(errs[0], "CNAME") {
		t.Fatalf("errors: %v", errs)
	}

	if _, err := c.Create(ctx, "links.alice.example"); !isCode(err, cloudflare.CodeDuplicate) {
		t.Fatalf("duplicate: %v", err)
	}
	found, err := c.Find(ctx, "links.alice.example")
	if err != nil || found.ID != h.ID {
		t.Fatalf("find: %+v %v", found, err)
	}
	if _, err := c.Find(ctx, "nope.example"); !errors.Is(err, cloudflare.ErrNotFound) {
		t.Fatalf("find missing: %v", err)
	}

	api.Set("links.alice.example", "active", "active")
	got, err := c.Get(ctx, h.ID)
	if err != nil || !got.Active() || len(got.Errors()) != 0 {
		t.Fatalf("get: %+v %v", got, err)
	}

	if err := c.Delete(ctx, h.ID); err != nil {
		t.Fatal(err)
	}
	if err := c.Delete(ctx, h.ID); !errors.Is(err, cloudflare.ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
	if _, err := c.Get(ctx, h.ID); !errors.Is(err, cloudflare.ErrNotFound) {
		t.Fatalf("get deleted: %v", err)
	}
}

func TestErrors(t *testing.T) {
	api := cftest.New(t)
	api.FailCreate = http.StatusBadRequest
	api.FailCreateMsg = "Hostname is not allowed."
	c := api.Client()
	_, err := c.Create(context.Background(), "x.example")
	var ae *cloudflare.APIError
	if !errors.As(err, &ae) || ae.Status != 400 || ae.Message() != "Hostname is not allowed." {
		t.Fatalf("create failure: %v", err)
	}

	c.Token = "wrong"
	if _, err := c.Get(context.Background(), "x"); !errors.As(err, &ae) || ae.Status != 403 || !strings.Contains(err.Error(), "Authentication error") {
		t.Fatalf("auth failure: %v", err)
	}

	// Non-JSON bodies and timeouts surface as errors, not panics.
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow/zones/z/custom_hostnames/x" {
			time.Sleep(200 * time.Millisecond)
		}
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte("<html>bad gateway</html>")) //nolint:errcheck
	}))
	defer bad.Close()
	c2 := &cloudflare.Client{BaseURL: bad.URL, Token: "t", ZoneID: "z", HTTP: &http.Client{Timeout: 50 * time.Millisecond}}
	if _, err := c2.Get(context.Background(), "x"); !errors.As(err, &ae) || ae.Status != 502 {
		t.Fatalf("html error: %v", err)
	}
	c2.BaseURL = bad.URL + "/slow"
	if _, err := c2.Get(context.Background(), "x"); err == nil || errors.As(err, &ae) {
		t.Fatalf("timeout: %v", err)
	}
}

func isCode(err error, code int) bool {
	var ae *cloudflare.APIError
	return errors.As(err, &ae) && ae.HasCode(code)
}
