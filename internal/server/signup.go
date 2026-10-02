package server

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"git.subcult.tv/subculture-collective/zelda/internal/blocks"
	"git.subcult.tv/subculture-collective/zelda/internal/brevo"
	"git.subcult.tv/subculture-collective/zelda/internal/config"
	"git.subcult.tv/subculture-collective/zelda/internal/mail"
	"git.subcult.tv/subculture-collective/zelda/internal/render"
	"git.subcult.tv/subculture-collective/zelda/internal/seal"
	"git.subcult.tv/subculture-collective/zelda/internal/store"
)

// Email sign-up blocks (#11): public double opt-in, the creator's list in
// the editor, and delivery to the creator's Brevo list.

// blockExtras holds state for the email sign-up and latest blocks.
type blockExtras struct {
	sealer         *seal.Sealer // nil when ZELDA_SECRET_KEY is unset
	brevo          *brevo.Client
	subscribeLimit *limiter

	youtubeBase string // https://www.youtube.com; tests point it at a fixture server
	feedTTL     time.Duration
	feedRetry   time.Duration
	feedWait    time.Duration // how long a render waits for a block's first fetch
	mu          sync.Mutex
	inflight    map[string]chan struct{}
	slots       chan struct{} // bounds concurrent background refreshes
	wg          sync.WaitGroup
}

func newBlockExtras(cfg *config.Config) *blockExtras {
	x := &blockExtras{
		brevo:          brevo.New(),
		subscribeLimit: newLimiter(10, time.Hour),
		youtubeBase:    "https://www.youtube.com",
		feedTTL:        time.Hour,
		feedRetry:      10 * time.Minute,
		feedWait:       time.Second,
		inflight:       map[string]chan struct{}{},
		slots:          make(chan struct{}, 8),
	}
	if len(cfg.SecretKey) > 0 {
		x.sealer, _ = seal.New(cfg.SecretKey)
	}
	return x
}

// SetBrevo replaces the Brevo client (tests use a fake API server).
func (s *Server) SetBrevo(c *brevo.Client) { s.x.brevo = c }

// SetSecretKey replaces the key that seals stored API keys; nil disables
// Brevo destinations.
func (s *Server) SetSecretKey(key []byte) error {
	if key == nil {
		s.x.sealer = nil
		return nil
	}
	sl, err := seal.New(key)
	if err != nil {
		return err
	}
	s.x.sealer = sl
	return nil
}

// brevoPushGiveUp is how long a confirmed address waits for Brevo before
// it is dropped.
const brevoPushGiveUp = 7 * 24 * time.Hour

func signupData(raw json.RawMessage) blocks.Signup {
	var d blocks.Signup
	_ = json.Unmarshal(raw, &d)
	return d
}

func blockAAD(blockID string) []byte { return []byte("signup-brevo-key:" + blockID) }

// prepareSignup applies the write-only Brevo key to a validated sign-up
// block: it checks the key and list with Brevo and sets brevo_key_set. The
// returned function stores or removes the sealed key once the block has an
// ID. It writes an error response and returns ok=false on failure.
func (s *Server) prepareSignup(w http.ResponseWriter, r *http.Request, existing *store.Block, data json.RawMessage,
	secrets map[string]string) (json.RawMessage, func(context.Context, *store.Block) error, bool) {
	d := signupData(data)
	key, newKey := secrets["brevo_api_key"]
	if d.Destination != blocks.DestBrevo {
		return data, func(ctx context.Context, b *store.Block) error {
			if existing == nil {
				return nil
			}
			return s.store.DeleteSignupSecret(ctx, b.ID)
		}, true
	}
	if s.x.sealer == nil {
		writeErr(w, http.StatusUnprocessableEntity, "unavailable",
			"sending sign-ups to Brevo isn't available on this server; keep the list in "+s.cfg.AppName+" instead", "destination")
		return nil, nil, false
	}
	var prev blocks.Signup
	if existing != nil && existing.Type == "email_signup" {
		prev = signupData(existing.Data)
	}
	if newKey {
		if len(key) > 200 || strings.ContainsAny(key, " \t\r\n") {
			writeErr(w, http.StatusUnprocessableEntity, "invalid", "that doesn't look like a Brevo API key", "brevo_api_key")
			return nil, nil, false
		}
	} else {
		if existing == nil {
			writeErr(w, http.StatusUnprocessableEntity, "invalid", "paste your Brevo API key", "brevo_api_key")
			return nil, nil, false
		}
		stored, err := s.brevoKey(r.Context(), existing.ID)
		if errors.Is(err, store.ErrNotFound) || errors.Is(err, seal.ErrOpen) {
			writeErr(w, http.StatusUnprocessableEntity, "invalid", "paste your Brevo API key", "brevo_api_key")
			return nil, nil, false
		} else if err != nil {
			s.serverErr(w, r, err)
			return nil, nil, false
		}
		key = stored
	}
	if newKey || prev.Destination != blocks.DestBrevo || prev.BrevoListID != d.BrevoListID {
		if !s.previewLimit.allow(userFrom(r).ID) {
			writeErr(w, http.StatusTooManyRequests, "rate_limited", "too many checks, try again later", "")
			return nil, nil, false
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		err := s.x.brevo.CheckList(ctx, key, int64(d.BrevoListID))
		cancel()
		switch {
		case errors.Is(err, brevo.ErrUnauthorized):
			writeErr(w, http.StatusUnprocessableEntity, "brevo_key", "Brevo rejected that API key", "brevo_api_key")
			return nil, nil, false
		case errors.Is(err, brevo.ErrListNotFound):
			writeErr(w, http.StatusUnprocessableEntity, "brevo_list", "your Brevo account has no list with that ID", "brevo_list_id")
			return nil, nil, false
		case err != nil:
			s.log.Warn("brevo list check", "err", err)
			writeErr(w, http.StatusUnprocessableEntity, "brevo_unavailable", "couldn't reach Brevo to check the key; try again", "brevo_api_key")
			return nil, nil, false
		}
	}
	d.BrevoKeySet = true
	out, err := json.Marshal(d)
	if err != nil {
		s.serverErr(w, r, err)
		return nil, nil, false
	}
	return out, func(ctx context.Context, b *store.Block) error {
		if !newKey {
			return nil
		}
		return s.store.SetSignupSecret(ctx, b.ID, s.x.sealer.Seal([]byte(key), blockAAD(b.ID)))
	}, true
}

func (s *Server) brevoKey(ctx context.Context, blockID string) (string, error) {
	if s.x.sealer == nil {
		return "", seal.ErrOpen
	}
	box, err := s.store.SignupSecret(ctx, blockID)
	if err != nil {
		return "", err
	}
	key, err := s.x.sealer.Open(box, blockAAD(blockID))
	return string(key), err
}

// pushToBrevo adds a confirmed address to the block's Brevo list.
func (s *Server) pushToBrevo(ctx context.Context, b *store.Block, email string) error {
	key, err := s.brevoKey(ctx, b.ID)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return s.x.brevo.AddContact(ctx, key, int64(signupData(b.Data).BrevoListID), email)
}

const subscribeAccepted = "Check your inbox for a link to confirm your subscription."

// handleSubscribe starts a double opt-in for a sign-up block. The response
// is the same whether or not the address is already on the list.
func (s *Server) handleSubscribe(w http.ResponseWriter, r *http.Request) {
	if !s.x.subscribeLimit.allow(s.clientIP(r)) {
		writeErr(w, http.StatusTooManyRequests, "rate_limited", "too many sign-ups from here, try again later", "")
		return
	}
	var body struct {
		Email   string `json:"email"`
		Website string `json:"website"` // honeypot: people never see this field
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	accepted := func() { writeJSON(w, http.StatusAccepted, map[string]string{"message": subscribeAccepted}) }
	id := r.PathValue("id")
	if !uuidRe.MatchString(id) {
		writeErr(w, http.StatusNotFound, "not_found", "this sign-up form isn't available", "")
		return
	}
	if strings.TrimSpace(body.Website) != "" {
		accepted()
		return
	}
	email := strings.TrimSpace(body.Email)
	if len(email) > 254 || !emailRe.MatchString(email) {
		writeErr(w, http.StatusUnprocessableEntity, "invalid", "enter a valid email address", "email")
		return
	}
	b, p, err := s.store.LiveBlock(r.Context(), id, s.now())
	if errors.Is(err, store.ErrNotFound) || (err == nil && b.Type != "email_signup") {
		writeErr(w, http.StatusNotFound, "not_found", "this sign-up form isn't available", "")
		return
	} else if err != nil {
		s.serverErr(w, r, err)
		return
	}
	d := signupData(b.Data)
	if d.Destination == blocks.DestBrevo && (!d.BrevoKeySet || s.x.sealer == nil) {
		writeErr(w, http.StatusServiceUnavailable, "unavailable", "this sign-up form isn't working right now", "")
		return
	}
	// Past the per-address limit, answer as usual but send nothing, so the
	// form can't be used to flood someone's inbox.
	if !s.mailLimit.allow("subscribe:" + id + ":" + strings.ToLower(email)) {
		accepted()
		return
	}
	tok, err := s.store.RequestSignup(r.Context(), id, email, s.now())
	switch {
	case errors.Is(err, store.ErrConflict):
		accepted()
		return
	case errors.Is(err, store.ErrLimit):
		writeErr(w, http.StatusTooManyRequests, "list_full", "this list isn't taking new sign-ups right now", "")
		return
	case err != nil:
		s.serverErr(w, r, err)
		return
	}
	s.sendMail(s.confirmationMail(p, d, email, tok))
	accepted()
}

func (s *Server) confirmationMail(p *store.Page, d blocks.Signup, email, tok string) mail.Message {
	link := s.cfg.BaseURL + "/subscribe/confirm?token=" + tok
	who := "The page owner can then see and download your address in " + s.cfg.AppName + "."
	if d.Destination == blocks.DestBrevo {
		who = "Your address will then be added to the page owner's mailing list at Brevo, their email provider."
	}
	return mail.Message{To: email, Subject: "Confirm your subscription: " + oneLineClip(d.Title, 80),
		Body: "Someone, probably you, asked to join \"" + d.Title + "\" on " + p.Title + "'s page (" + s.pageURL(p.Slug) + ").\n\n" +
			"Confirm here:\n\n" + link + "\n\n" + who + "\n\n" +
			"If you didn't ask for this, ignore this email. Unconfirmed addresses are deleted after 48 hours.\n"}
}

func oneLineClip(s string, n int) string {
	return clipText(strings.NewReplacer("\r", " ", "\n", " ").Replace(s), n)
}

// handleSubscribeConfirm shows a confirm button (GET) and confirms (POST).
// Confirming takes a POST so link scanners that open the GET URL don't
// confirm addresses on their own.
func (s *Server) handleSubscribeConfirm(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	page := render.Simple{AppName: s.cfg.AppName, BaseURL: s.cfg.BaseURL}
	if !s.verifyLimit.allow("subscribe:" + s.clientIP(r)) {
		page.Title, page.Heading, page.Message = "Try again later", "Too many requests", "Wait a few minutes and open the link again."
		s.writeSimple(w, http.StatusTooManyRequests, page)
		return
	}
	if r.Method == http.MethodGet {
		tok := r.URL.Query().Get("token")
		if tok == "" || len(tok) > 100 {
			page.Title, page.Heading, page.Message = "Link not valid", "This link isn't valid", "Sign up again from the page to get a new link."
			s.writeSimple(w, http.StatusBadRequest, page)
			return
		}
		page.Title, page.Heading = "Confirm your subscription", "Confirm your subscription"
		page.Message = "Press the button to join the mailing list."
		page.Form = &render.SimpleForm{Action: "/subscribe/confirm", Token: tok, Button: "Confirm subscription"}
		s.writeSimple(w, http.StatusOK, page)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	c, err := s.store.ConfirmSignup(r.Context(), r.PostFormValue("token"), s.now())
	if errors.Is(err, store.ErrNotFound) {
		page.Title, page.Heading = "Link not valid", "This link has expired or was already used"
		page.Message = "If you already confirmed, you're on the list. Otherwise sign up again from the page."
		s.writeSimple(w, http.StatusBadRequest, page)
		return
	} else if err != nil {
		s.log.Error("confirm signup", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	title := s.cfg.AppName
	if b, p, err := s.store.BlockByID(r.Context(), c.BlockID); err == nil {
		title = p.Title
		if signupData(b.Data).Destination == blocks.DestBrevo {
			s.deliverToBrevo(r.Context(), b, c.ID, c.Email)
		}
	}
	page.Title, page.Heading = "Subscribed", "You're subscribed"
	page.Message = "You're on " + title + "'s mailing list."
	s.writeSimple(w, http.StatusOK, page)
}

// deliverToBrevo sends a confirmed address to Brevo and deletes it here. If
// Brevo fails, the address is kept for RunSignupJobs to retry.
func (s *Server) deliverToBrevo(ctx context.Context, b *store.Block, contactID, email string) {
	if err := s.pushToBrevo(ctx, b, email); err != nil {
		s.log.Warn("brevo delivery failed; will retry", "block", b.ID, "err", err)
		if err := s.store.SetSignupPendingPush(ctx, contactID, true); err != nil {
			s.log.Error("mark brevo retry", "err", err)
		}
		return
	}
	if err := s.store.DeleteSignupContact(ctx, contactID); err != nil {
		s.log.Error("delete delivered contact", "err", err)
	}
}

// SignupJobsOnce deletes expired confirmation requests and retries Brevo
// deliveries. Addresses Brevo has not accepted after a week are dropped.
func (s *Server) SignupJobsOnce(ctx context.Context) error {
	now := s.now()
	if n, err := s.store.PurgeExpiredSignups(ctx, now); err != nil {
		return err
	} else if n > 0 {
		s.log.Info("deleted unconfirmed sign-ups", "count", n)
	}
	list, err := s.store.PendingPushes(ctx, 200)
	if err != nil {
		return err
	}
	for _, c := range list {
		b, _, err := s.store.BlockByID(ctx, c.BlockID)
		if err != nil {
			continue
		}
		if b.Type != "email_signup" || signupData(b.Data).Destination != blocks.DestBrevo {
			// The creator switched the block to keep the list in Kork.
			_ = s.store.SetSignupPendingPush(ctx, c.ID, false)
			continue
		}
		if err := s.pushToBrevo(ctx, b, c.Email); err == nil {
			_ = s.store.DeleteSignupContact(ctx, c.ID)
		} else if now.Sub(c.ConfirmedAt) > brevoPushGiveUp {
			s.log.Warn("dropping sign-up Brevo never accepted", "block", b.ID, "err", err)
			_ = s.store.DeleteSignupContact(ctx, c.ID)
		}
	}
	return nil
}

// RunSignupJobs calls SignupJobsOnce every interval until ctx is done.
func (s *Server) RunSignupJobs(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if err := s.SignupJobsOnce(ctx); err != nil && ctx.Err() == nil {
			s.log.Warn("sign-up jobs", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// ---- editor: the creator's list ----

func (s *Server) ownedSignupBlock(w http.ResponseWriter, r *http.Request) *store.Block {
	p := s.ownedPage(w, r)
	if p == nil {
		return nil
	}
	id := r.PathValue("blockID")
	if !uuidRe.MatchString(id) {
		writeErr(w, http.StatusNotFound, "not_found", "block not found", "")
		return nil
	}
	b, err := s.store.Block(r.Context(), p.ID, id)
	if errors.Is(err, store.ErrNotFound) || (err == nil && b.Type != "email_signup") {
		writeErr(w, http.StatusNotFound, "not_found", "block not found", "")
		return nil
	} else if err != nil {
		s.serverErr(w, r, err)
		return nil
	}
	return b
}

func (s *Server) handleListContacts(w http.ResponseWriter, r *http.Request) {
	b := s.ownedSignupBlock(w, r)
	if b == nil {
		return
	}
	list, counts, err := s.store.SignupContacts(r.Context(), b.ID, s.now())
	if err != nil {
		s.serverErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"contacts": list, "pending": counts.Pending, "awaiting_push": counts.AwaitingPush})
}

// csvCell stops spreadsheet apps from running a value as a formula.
func csvCell(v string) string {
	if v != "" && strings.ContainsRune("=+-@\t\r", rune(v[0])) {
		return "'" + v
	}
	return v
}

func (s *Server) handleContactsCSV(w http.ResponseWriter, r *http.Request) {
	b := s.ownedSignupBlock(w, r)
	if b == nil {
		return
	}
	list, _, err := s.store.SignupContacts(r.Context(), b.ID, s.now())
	if err != nil {
		s.serverErr(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+safeFilename(signupData(b.Data).Title)+`-contacts.csv"`)
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"email", "confirmed_at"})
	for _, c := range list {
		_ = cw.Write([]string{csvCell(c.Email), c.ConfirmedAt.UTC().Format(time.RFC3339)})
	}
	cw.Flush()
}

func (s *Server) handleDeleteContact(w http.ResponseWriter, r *http.Request) {
	b := s.ownedSignupBlock(w, r)
	if b == nil {
		return
	}
	id := r.PathValue("contactID")
	if !uuidRe.MatchString(id) {
		writeErr(w, http.StatusNotFound, "not_found", "contact not found", "")
		return
	}
	if err := s.store.DeleteBlockContact(r.Context(), b.ID, id); errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not_found", "contact not found", "")
		return
	} else if err != nil {
		s.serverErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
