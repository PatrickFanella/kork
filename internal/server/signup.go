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
	"git.subcult.tv/subculture-collective/zelda/internal/fetch"
	"git.subcult.tv/subculture-collective/zelda/internal/listmonk"
	"git.subcult.tv/subculture-collective/zelda/internal/mail"
	"git.subcult.tv/subculture-collective/zelda/internal/render"
	"git.subcult.tv/subculture-collective/zelda/internal/seal"
	"git.subcult.tv/subculture-collective/zelda/internal/store"
)

// Email sign-up blocks (#11): public double opt-in, the creator's list in
// the editor, and delivery to the creator's Brevo or listmonk list.

// blockExtras holds state for the email sign-up and latest blocks.
type blockExtras struct {
	sealer         *seal.Sealer // nil when ZELDA_SECRET_KEY is unset
	brevo          *brevo.Client
	listmonk       *listmonk.Client // nil: use the server's fetcher
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

// SetListmonk replaces the listmonk client (tests use a fake API server).
func (s *Server) SetListmonk(c *listmonk.Client) { s.x.listmonk = c }

// listmonkClient calls a creator's listmonk through the fetcher, which
// refuses private addresses and doesn't follow redirects with the token.
func (s *Server) listmonkClient() *listmonk.Client {
	if s.x.listmonk != nil {
		return s.x.listmonk
	}
	return &listmonk.Client{HTTP: s.fetcher}
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

// brevoPushGiveUp is how long Brevo can keep refusing a confirmed address
// before it is dropped. Failures on Kork's side, such as a key that can't be
// unsealed, never drop an address.
const brevoPushGiveUp = 7 * 24 * time.Hour

// Retry spacing for Brevo deliveries: 15 minutes after the first failure,
// doubling up to 12 hours.
const (
	brevoRetryFirst = 15 * time.Minute
	brevoRetryMax   = 12 * time.Hour
)

func brevoRetryAfter(attempts int) time.Duration {
	d := brevoRetryFirst
	for i := 0; i < attempts && d < brevoRetryMax; i++ {
		d *= 2
	}
	return min(d, brevoRetryMax)
}

// pushProblem turns a delivery error into a message safe to show the page
// owner and reports whether the provider itself refused the request.
func pushProblem(dest string, err error) (msg string, rejected bool) {
	name, key := "Brevo", "API key"
	if dest == blocks.DestListmonk {
		name, key = "listmonk", "API token"
	}
	switch {
	case errors.Is(err, errListChanged):
		return "Some addresses were confirmed for a list setting you've since changed, so they won't be sent. " +
			"They're deleted 7 days after confirming unless you switch back.", true
	case errors.Is(err, seal.ErrOpen), errors.Is(err, store.ErrNotFound):
		return "The saved " + name + " " + key + " can't be read on this server. Enter the " + key + " again to resume sending.", false
	case errors.Is(err, brevo.ErrUnauthorized), errors.Is(err, listmonk.ErrUnauthorized):
		return name + " rejected the " + key + ". Enter a new one.", true
	case errors.Is(err, brevo.ErrListNotFound), errors.Is(err, listmonk.ErrListNotFound):
		return name + " has no list with this ID. Check the list ID.", true
	case errors.Is(err, fetch.ErrBlocked), errors.Is(err, fetch.ErrURL):
		return "Kork can't reach that listmonk address. It must be a public https address.", false
	case brevo.Rejected(err), listmonk.Rejected(err):
		return name + " refused the address.", true
	}
	return name + " couldn't be reached. Sending will be retried.", false
}

// errListChanged: the block now sends to a different list than the one in
// the address's consent receipt, so the address is not sent.
var errListChanged = errors.New("signup: list changed since the visitor agreed")

func signupData(raw json.RawMessage) blocks.Signup {
	var d blocks.Signup
	_ = json.Unmarshal(raw, &d)
	return d
}

func blockAAD(blockID string) []byte { return []byte("signup-brevo-key:" + blockID) }

// listmonkAAD binds a sealed listmonk token to its block. It differs from
// blockAAD, so a stored Brevo key can never be opened as a listmonk token.
func listmonkAAD(blockID string) []byte { return []byte("signup-listmonk-token:" + blockID) }

// prepareSignup applies the write-only Brevo key to a validated sign-up
// block: it checks the key and list with Brevo and sets brevo_key_set. The
// returned function stores or removes the sealed key once the block has an
// ID. It writes an error response and returns ok=false on failure.
func (s *Server) prepareSignup(w http.ResponseWriter, r *http.Request, existing *store.Block, data json.RawMessage,
	secrets map[string]string) (json.RawMessage, func(context.Context, *store.Block) error, bool) {
	d := signupData(data)
	if d.Destination == blocks.DestListmonk {
		return s.prepareListmonk(w, r, existing, d, secrets)
	}
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

// prepareListmonk checks a listmonk destination: the API token is
// write-only and sealed per block, and the account and list are checked
// with the creator's listmonk whenever they change.
func (s *Server) prepareListmonk(w http.ResponseWriter, r *http.Request, existing *store.Block, d blocks.Signup,
	secrets map[string]string) (json.RawMessage, func(context.Context, *store.Block) error, bool) {
	if s.x.sealer == nil {
		writeErr(w, http.StatusUnprocessableEntity, "unavailable",
			"sending sign-ups to listmonk isn't available on this server; keep the list in "+s.cfg.AppName+" instead", "destination")
		return nil, nil, false
	}
	var prev blocks.Signup
	if existing != nil && existing.Type == "email_signup" {
		prev = signupData(existing.Data)
	}
	tok, newTok := secrets["listmonk_api_token"]
	if newTok {
		if len(tok) > 200 || strings.ContainsAny(tok, " \t\r\n:") {
			writeErr(w, http.StatusUnprocessableEntity, "invalid", "that doesn't look like a listmonk API token", "listmonk_api_token")
			return nil, nil, false
		}
	} else {
		var err error
		if existing != nil && prev.Destination == blocks.DestListmonk {
			tok, err = s.listmonkToken(r.Context(), existing.ID)
		} else {
			err = store.ErrNotFound
		}
		if errors.Is(err, store.ErrNotFound) || errors.Is(err, seal.ErrOpen) {
			writeErr(w, http.StatusUnprocessableEntity, "invalid", "paste your listmonk API token", "listmonk_api_token")
			return nil, nil, false
		} else if err != nil {
			s.serverErr(w, r, err)
			return nil, nil, false
		}
	}
	if newTok || prev.Destination != blocks.DestListmonk || prev.ListmonkURL != d.ListmonkURL ||
		prev.ListmonkListID != d.ListmonkListID || prev.ListmonkUser != d.ListmonkUser {
		if !s.previewLimit.allow(userFrom(r).ID) {
			writeErr(w, http.StatusTooManyRequests, "rate_limited", "too many checks, try again later", "")
			return nil, nil, false
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		err := s.listmonkClient().CheckList(ctx, listmonk.Account{BaseURL: d.ListmonkURL, User: d.ListmonkUser, Token: tok},
			int64(d.ListmonkListID))
		cancel()
		var ae *listmonk.APIError
		switch {
		case err == nil:
		case errors.Is(err, listmonk.ErrUnauthorized):
			writeErr(w, http.StatusUnprocessableEntity, "listmonk_key", "listmonk rejected that API user or token", "listmonk_api_token")
			return nil, nil, false
		case errors.Is(err, listmonk.ErrListNotFound):
			writeErr(w, http.StatusUnprocessableEntity, "listmonk_list", "your listmonk has no list with that ID", "listmonk_list_id")
			return nil, nil, false
		case errors.Is(err, fetch.ErrBlocked), errors.Is(err, fetch.ErrURL):
			writeErr(w, http.StatusUnprocessableEntity, "listmonk_url",
				"Kork can't reach that address; it must be a public https address on the standard port", "listmonk_url")
			return nil, nil, false
		case errors.As(err, &ae) && ae.Status >= 300 && ae.Status < 400:
			writeErr(w, http.StatusUnprocessableEntity, "listmonk_url",
				"that address redirects; enter the address your listmonk is served from", "listmonk_url")
			return nil, nil, false
		default:
			s.log.Warn("listmonk list check", "err", err)
			writeErr(w, http.StatusUnprocessableEntity, "listmonk_unavailable", "couldn't reach your listmonk to check it; try again", "listmonk_url")
			return nil, nil, false
		}
	}
	d.ListmonkKeySet = true
	out, err := json.Marshal(d)
	if err != nil {
		s.serverErr(w, r, err)
		return nil, nil, false
	}
	return out, func(ctx context.Context, b *store.Block) error {
		if !newTok {
			return nil
		}
		return s.store.SetSignupSecret(ctx, b.ID, s.x.sealer.Seal([]byte(tok), listmonkAAD(b.ID)))
	}, true
}

func (s *Server) listmonkToken(ctx context.Context, blockID string) (string, error) {
	if s.x.sealer == nil {
		return "", seal.ErrOpen
	}
	box, err := s.store.SignupSecret(ctx, blockID)
	if err != nil {
		return "", err
	}
	tok, err := s.x.sealer.Open(box, listmonkAAD(blockID))
	return string(tok), err
}

// pushToListmonk adds a confirmed address to the block's listmonk list. An
// address listmonk has as unsubscribed or blocklisted is left as it is and
// counts as delivered, so it is deleted here.
func (s *Server) pushToListmonk(ctx context.Context, b *store.Block, email string) error {
	d := signupData(b.Data)
	tok, err := s.listmonkToken(ctx, b.ID)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := s.listmonkClient().Subscribe(ctx, listmonk.Account{BaseURL: d.ListmonkURL, User: d.ListmonkUser, Token: tok},
		int64(d.ListmonkListID), email)
	if err == nil && out != listmonk.Added {
		s.log.Info("listmonk kept the address as it was", "block", b.ID, "outcome", string(out))
	}
	return err
}

// pushSignup sends a confirmed address to the block's external list.
func (s *Server) pushSignup(ctx context.Context, b *store.Block, email string) error {
	if signupData(b.Data).Destination == blocks.DestListmonk {
		return s.pushToListmonk(ctx, b, email)
	}
	return s.pushToBrevo(ctx, b, email)
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

// signupConsent is the receipt stored with each sign-up request: who asked
// the visitor (sender and page), for what (purpose), where confirmed
// addresses go (list) and the disclosure shown. It covers only this sender
// and list; it never implies permission for anyone else.
type signupConsent struct {
	Version    int       `json:"v"`
	List       string    `json:"list"`
	Sender     string    `json:"sender"`
	Page       string    `json:"page"`
	Purpose    string    `json:"purpose"`
	Disclosure string    `json:"disclosure"`
	At         time.Time `json:"at"`
}

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
	if (d.Destination == blocks.DestBrevo && !d.BrevoKeySet) || (d.Destination == blocks.DestListmonk && !d.ListmonkKeySet) ||
		(d.External() && s.x.sealer == nil) {
		writeErr(w, http.StatusServiceUnavailable, "unavailable", "this sign-up form isn't working right now", "")
		return
	}
	// Past the per-address limit, answer as usual but send nothing, so the
	// form can't be used to flood someone's inbox.
	if !s.mailLimit.allow("subscribe:" + id + ":" + strings.ToLower(email)) {
		accepted()
		return
	}
	consent, err := json.Marshal(signupConsent{Version: 1, List: d.ListKey(), Sender: p.Title, Page: s.pageURL(p.Slug),
		Purpose: d.Title, Disclosure: d.Disclosure(), At: s.now().UTC()})
	if err != nil {
		s.serverErr(w, r, err)
		return
	}
	tok, err := s.store.RequestSignup(r.Context(), id, email, consent, s.now())
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
	if d.External() {
		who = d.Disclosure()
	}
	return mail.Message{To: email, Subject: "Confirm your subscription: " + oneLineClip(d.Title, 80),
		Body: "Please confirm your subscription to \"" + d.Title + "\" on " + p.Title + "'s page (" + s.pageURL(p.Slug) + "). Someone entered this address there.\n\n" +
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
		if c.PendingPush {
			// The contact is already marked as waiting, so a failure here is
			// retried by RunSignupJobs. Don't let the visitor closing the tab
			// cancel the delivery.
			s.deliverSignup(context.WithoutCancel(r.Context()), b, store.PendingPush{ConfirmedSignup: *c, ConfirmedAt: s.now()})
		}
	}
	page.Title, page.Heading = "Subscribed", "You're subscribed"
	page.Message = "You're on " + title + "'s mailing list."
	s.writeSimple(w, http.StatusOK, page)
}

// deliverSignup sends a waiting address to the block's external list and
// deletes it here. An address is only sent to the list in its consent
// receipt. On failure the next attempt is scheduled; an address the provider
// has refused for longer than brevoPushGiveUp is dropped.
func (s *Server) deliverSignup(ctx context.Context, b *store.Block, c store.PendingPush) {
	d := signupData(b.Data)
	var err error
	if c.ListKey != "" && c.ListKey != d.ListKey() {
		err = errListChanged
	} else {
		err = s.pushSignup(ctx, b, c.Email)
	}
	if err == nil {
		if err := s.store.DeleteSignupContact(ctx, c.ID); err != nil {
			s.log.Error("delete delivered contact", "err", err)
		}
		return
	}
	now := s.now()
	msg, rejected := pushProblem(d.Destination, err)
	if rejected && now.Sub(c.ConfirmedAt) > brevoPushGiveUp {
		s.log.Warn("dropping sign-up that can't be delivered", "block", b.ID, "err", err)
		_ = s.store.DeleteSignupContact(ctx, c.ID)
		return
	}
	s.log.Warn("sign-up delivery failed; will retry", "block", b.ID, "destination", d.Destination, "rejected", rejected, "err", err)
	if err := s.store.RecordPushFailure(ctx, c.ID, now.Add(brevoRetryAfter(c.Attempts)), msg, rejected); err != nil {
		s.log.Error("record sign-up retry", "err", err)
	}
}

// SignupJobsOnce deletes expired confirmation requests and retries due
// deliveries to external lists.
func (s *Server) SignupJobsOnce(ctx context.Context) error {
	now := s.now()
	if n, err := s.store.PurgeExpiredSignups(ctx, now); err != nil {
		return err
	} else if n > 0 {
		s.log.Info("deleted unconfirmed sign-ups", "count", n)
	}
	list, err := s.store.PendingPushes(ctx, now, 200)
	if err != nil {
		return err
	}
	for _, c := range list {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		b, _, err := s.store.BlockByID(ctx, c.BlockID)
		if err != nil {
			continue
		}
		if c.ListKey == "" && (b.Type != "email_signup" || !signupData(b.Data).External()) {
			// From before consent receipts: the creator switched the block to
			// keep the list in Kork.
			_ = s.store.SetSignupPendingPush(ctx, c.ID, false)
			continue
		}
		s.deliverSignup(ctx, b, c)
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
	writeJSON(w, http.StatusOK, map[string]any{"contacts": list, "pending": counts.Pending, "awaiting_push": counts.AwaitingPush,
		"push_problem": counts.PushProblem})
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
