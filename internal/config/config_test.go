package config

import (
	"strings"
	"testing"
)

func TestBillingConfig(t *testing.T) {
	base := func(t *testing.T) {
		t.Setenv("ZELDA_DATABASE_URL", "postgres://x")
		for _, k := range []string{"ZELDA_SECRET_KEY", "ZELDA_STRIPE_SECRET_KEY", "ZELDA_STRIPE_WEBHOOK_SECRET",
			"ZELDA_STRIPE_PRICE_MONTHLY", "ZELDA_STRIPE_PRICE_ANNUAL", "ZELDA_PATREON_CLIENT_ID",
			"ZELDA_PATREON_CLIENT_SECRET", "ZELDA_PATREON_CAMPAIGN_ID", "ZELDA_PATREON_TIER_IDS"} {
			t.Setenv(k, "")
		}
	}
	stripe := func(t *testing.T) {
		t.Setenv("ZELDA_STRIPE_SECRET_KEY", "sk_test_1")
		t.Setenv("ZELDA_STRIPE_WEBHOOK_SECRET", "whsec_1")
		t.Setenv("ZELDA_STRIPE_PRICE_MONTHLY", "price_m")
		t.Setenv("ZELDA_STRIPE_PRICE_ANNUAL", "price_y")
	}
	patreon := func(t *testing.T) {
		t.Setenv("ZELDA_PATREON_CLIENT_ID", "cid")
		t.Setenv("ZELDA_PATREON_CLIENT_SECRET", "csecret")
		t.Setenv("ZELDA_PATREON_CAMPAIGN_ID", "123")
		t.Setenv("ZELDA_PATREON_TIER_IDS", "456, 789")
	}
	const key = "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="

	t.Run("off by default", func(t *testing.T) {
		base(t)
		c, err := Load()
		if err != nil || c.StripeEnabled() || c.PatreonEnabled() || c.SecretKey != nil {
			t.Fatalf("%+v %v", c, err)
		}
	})
	t.Run("stripe", func(t *testing.T) {
		base(t)
		stripe(t)
		c, err := Load()
		if err != nil || !c.StripeEnabled() || c.PatreonEnabled() {
			t.Fatalf("%v", err)
		}
	})
	t.Run("partial stripe", func(t *testing.T) {
		base(t)
		t.Setenv("ZELDA_STRIPE_SECRET_KEY", "sk_test_1")
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), "ZELDA_STRIPE_WEBHOOK_SECRET") {
			t.Fatalf("partial stripe accepted: %v", err)
		}
	})
	t.Run("patreon", func(t *testing.T) {
		base(t)
		patreon(t)
		t.Setenv("ZELDA_SECRET_KEY", key)
		c, err := Load()
		if err != nil || !c.PatreonEnabled() || len(c.PatreonTierIDs) != 2 || c.PatreonTierIDs[1] != "789" {
			t.Fatalf("%+v %v", c, err)
		}
	})
	t.Run("patreon needs secret key", func(t *testing.T) {
		base(t)
		patreon(t)
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), "ZELDA_SECRET_KEY") {
			t.Fatalf("patreon without key: %v", err)
		}
	})
	t.Run("bad secret key", func(t *testing.T) {
		base(t)
		t.Setenv("ZELDA_SECRET_KEY", "too-short")
		if _, err := Load(); err == nil {
			t.Fatal("bad key accepted")
		}
	})
}
