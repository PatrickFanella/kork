package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"git.subcult.tv/subculture-collective/zelda/internal/billing"
	"git.subcult.tv/subculture-collective/zelda/internal/store"
)

// maxWebhookBody caps a Stripe event payload.
const maxWebhookBody = 1 << 20

// handleStripeWebhook receives Stripe events. It has no session or CSRF
// header; the Stripe-Signature header authenticates it. Responses: 400 for
// a bad signature or body, 500 when processing failed and Stripe should
// retry, otherwise 200.
func (s *Server) handleStripeWebhook(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.StripeEnabled() {
		http.NotFound(w, r)
		return
	}
	payload, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhookBody))
	if err != nil {
		http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
		return
	}
	if err := billing.VerifySignature(payload, r.Header.Get("Stripe-Signature"), s.cfg.StripeWebhookSecret,
		billing.SignatureTolerance, s.now()); err != nil {
		s.log.Warn("stripe webhook rejected", "err", err)
		http.Error(w, "invalid signature", http.StatusBadRequest)
		return
	}
	var ev billing.Event
	if err := json.Unmarshal(payload, &ev); err != nil || ev.ID == "" || ev.Type == "" {
		http.Error(w, "invalid event", http.StatusBadRequest)
		return
	}
	err = s.handleStripeEvent(r.Context(), &ev)
	switch {
	case errors.Is(err, store.ErrNoBillingAccount):
		s.log.Warn("stripe event for unknown account", "event", ev.ID, "type", ev.Type)
	case errors.Is(err, store.ErrConflict):
		// A customer or subscription already linked to another account;
		// retrying won't help, so acknowledge and leave it for a person.
		s.log.Error("stripe event conflicts with another account", "event", ev.ID, "type", ev.Type)
	case err != nil:
		s.log.Error("stripe event failed", "event", ev.ID, "type", ev.Type, "err", err)
		http.Error(w, "processing failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"received": true})
}

// handleStripeEvent applies one event. Each event ID is applied once.
func (s *Server) handleStripeEvent(ctx context.Context, ev *billing.Event) error {
	at := time.Unix(ev.Created, 0).UTC()
	switch ev.Type {
	case "checkout.session.completed":
		var cs billing.CheckoutSession
		if err := json.Unmarshal(ev.Data.Object, &cs); err != nil {
			return err
		}
		if cs.Mode != "subscription" || cs.Subscription == "" {
			return nil
		}
		if seen, err := s.store.StripeEventSeen(ctx, ev.ID); err != nil || seen {
			return err
		}
		// The session doesn't carry the subscription's status or period, and
		// events can arrive in any order, so read the subscription now.
		sub, err := s.bill.stripe.GetSubscription(ctx, string(cs.Subscription))
		if err != nil {
			return err
		}
		userID := cs.ClientReferenceID
		if userID == "" {
			userID = cs.Metadata["user_id"]
		}
		if sub.Customer == "" {
			sub.Customer = cs.Customer
		}
		return s.applyStripeSubscription(ctx, ev,
			store.StripeTarget{UserID: userID, CustomerID: string(cs.Customer), SubscriptionID: sub.ID}, sub, at, true)

	case "customer.subscription.created", "customer.subscription.updated", "customer.subscription.deleted":
		var sub billing.Subscription
		if err := json.Unmarshal(ev.Data.Object, &sub); err != nil {
			return err
		}
		return s.applyStripeSubscription(ctx, ev,
			store.StripeTarget{UserID: sub.Metadata["user_id"], CustomerID: string(sub.Customer), SubscriptionID: sub.ID}, &sub, at, false)

	case "invoice.payment_failed":
		var inv billing.Invoice
		if err := json.Unmarshal(ev.Data.Object, &inv); err != nil {
			return err
		}
		subID := inv.SubscriptionID()
		if subID == "" {
			return nil
		}
		_, err := s.store.ApplyStripeEvent(ctx, ev.ID, ev.Type,
			store.StripeTarget{CustomerID: string(inv.Customer), SubscriptionID: subID},
			func(b *store.Billing) error {
				// The plan follows the subscription status (past_due keeps Pro
				// until the period ends); this only drives the editor notice.
				if b.StripeSubscriptionID == subID {
					b.StripePaymentFailed = &at
				}
				return nil
			})
		return err
	}
	return nil // other event types are not used
}

// Reasons applyStripeSubscription stops before saving. Returning an error
// from the ApplyStripeEvent callback rolls back the transaction, so the
// event is not yet recorded as seen.
var (
	// errStripeTie: the event has the same timestamp as the last one applied
	// to this subscription. Created has one-second resolution, so the order
	// is unknown and the subscription is read from the API instead.
	errStripeTie = errors.New("stripe: event in the same second as the last one applied")
	// errDuplicateSubscription: a second live subscription for an account
	// that already has one, such as two checkouts paid from two tabs.
	errDuplicateSubscription = errors.New("stripe: second live subscription for the account")
)

// applyStripeSubscription applies a subscription event to the account. When
// the event ties with the last one applied, it applies the subscription as
// read from the API. When the account already has a different live
// subscription, it cancels the new one so the customer isn't charged twice;
// the duplicate's first payment needs a refund in the Stripe dashboard.
func (s *Server) applyStripeSubscription(ctx context.Context, ev *billing.Event, t store.StripeTarget,
	sub *billing.Subscription, at time.Time, fresh bool) error {
	var tracked, userID string
	apply := func(sub *billing.Subscription, fresh bool) error {
		_, err := s.store.ApplyStripeEvent(ctx, ev.ID, ev.Type, t, func(b *store.Billing) error {
			if b.StripeSubscriptionID != "" && b.StripeSubscriptionID != sub.ID &&
				billing.Subscribed(b.StripeStatus) && billing.Subscribed(sub.Status) {
				tracked, userID = b.StripeSubscriptionID, b.UserID
				return errDuplicateSubscription
			}
			if !fresh && b.StripeSubscriptionID == sub.ID && b.StripeEventAt != nil && at.Equal(*b.StripeEventAt) {
				return errStripeTie
			}
			applySubscription(b, sub, at, fresh)
			return nil
		})
		return err
	}
	err := apply(sub, fresh)
	if errors.Is(err, errStripeTie) {
		cur, gerr := s.bill.stripe.GetSubscription(ctx, sub.ID)
		if gerr != nil {
			return gerr // 500: Stripe retries the event
		}
		if cur.Customer == "" {
			cur.Customer = sub.Customer
		}
		sub, err = cur, apply(cur, true)
	}
	if !errors.Is(err, errDuplicateSubscription) {
		return err
	}
	// Confirm the tracked subscription is still live; a missed event could
	// have left a canceled one looking active here.
	old, gerr := s.bill.stripe.GetSubscription(ctx, tracked)
	var ae *billing.APIError
	switch {
	case errors.As(gerr, &ae) && ae.Status == http.StatusNotFound:
	case gerr != nil:
		return gerr
	case billing.Subscribed(old.Status):
		s.log.Error("stripe: canceling a duplicate subscription; refund its payment in the Stripe dashboard",
			"event", ev.ID, "kept", tracked, "canceled", sub.ID, "customer", string(sub.Customer))
		if err := s.bill.stripe.CancelSubscription(ctx, sub.ID); err != nil {
			return err
		}
		// Record the event as handled without changing the account.
		_, err := s.store.ApplyStripeEvent(ctx, ev.ID, ev.Type, t, func(*store.Billing) error { return nil })
		return err
	}
	// The tracked subscription has ended: store its real state so the new
	// one can replace it.
	if err := s.store.UpdateBilling(ctx, userID, func(b *store.Billing) error {
		if b.StripeSubscriptionID != tracked {
			return nil // changed meanwhile; the retry below decides again
		}
		if gerr != nil { // Stripe no longer has it
			b.StripeStatus = "canceled"
			return nil
		}
		applySubscription(b, old, at, true)
		return nil
	}); err != nil {
		return err
	}
	return apply(sub, true)
}

// applySubscription records a subscription's state and the Pro grant it
// implies. Events older than the newest one applied are skipped unless the
// subscription was just fetched from the API (fresh). A different
// subscription only replaces the stored one when it is live, so a stray
// incomplete checkout can't hide an active subscription.
func applySubscription(b *store.Billing, sub *billing.Subscription, at time.Time, fresh bool) {
	if b.StripeSubscriptionID != "" && b.StripeSubscriptionID != sub.ID && !billing.Subscribed(sub.Status) {
		return
	}
	if !fresh && b.StripeSubscriptionID == sub.ID && b.StripeEventAt != nil && at.Before(*b.StripeEventAt) {
		return
	}
	if sub.Customer != "" {
		b.StripeCustomerID = string(sub.Customer)
	}
	b.StripeSubscriptionID = sub.ID
	b.StripeStatus = sub.Status
	b.StripePriceID = sub.PriceID()
	b.StripeCancelAtEnd = sub.CancelAtPeriodEnd
	b.StripePeriodEnd = nil
	end := sub.PeriodEnd()
	if !end.IsZero() {
		b.StripePeriodEnd = &end
	}
	if b.StripeEventAt == nil || at.After(*b.StripeEventAt) {
		b.StripeEventAt = &at
	}
	b.StripeProUntil = billing.StripeProUntil(sub.Status, end, b.StripeProUntil)
	if sub.Status == "active" || sub.Status == "trialing" {
		b.StripePaymentFailed = nil
	}
}
