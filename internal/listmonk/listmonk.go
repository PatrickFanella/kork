// Package listmonk adds confirmed sign-ups to a creator's own listmonk
// list (https://listmonk.app/docs/apis/). It is a narrow adapter: it checks
// that a list exists and adds one address to it, and it never re-subscribes
// an address the list owner's listmonk has unsubscribed or blocklisted.
package listmonk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

var (
	// ErrUnauthorized means listmonk rejected the API user or token.
	ErrUnauthorized = errors.New("listmonk rejected the API user or token")
	// ErrListNotFound means the list ID does not exist on that listmonk.
	ErrListNotFound = errors.New("listmonk has no list with that ID")
)

// APIError is a listmonk response with an unexpected status.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("listmonk: %d: %s", e.Status, e.Message)
}

// Rejected reports whether listmonk itself refused the request in a way a
// retry won't fix. Network failures, 429 and 5xx are not rejections.
func Rejected(err error) bool {
	var ae *APIError
	switch {
	case errors.Is(err, ErrUnauthorized), errors.Is(err, ErrListNotFound):
		return true
	case errors.As(err, &ae):
		return ae.Status >= 400 && ae.Status < 500 && ae.Status != http.StatusTooManyRequests
	}
	return false
}

// Outcome is what Subscribe did.
type Outcome string

const (
	// Added: the address is now on the list as confirmed.
	Added Outcome = "added"
	// AlreadyOnList: the address was already on the list; nothing changed.
	AlreadyOnList Outcome = "already_on_list"
	// Unsubscribed: the address had unsubscribed from this list. It is left
	// unsubscribed; only the person can resubscribe through listmonk.
	Unsubscribed Outcome = "unsubscribed"
	// Blocklisted: the list owner blocklisted the address; nothing changed.
	Blocklisted Outcome = "blocklisted"
)

// Doer sends a request and returns its status and body. fetch.Client
// implements it with address checks and without following redirects.
type Doer interface {
	Do(req *http.Request, maxBytes int64) (int, []byte, error)
}

// Client calls a creator's listmonk API.
type Client struct {
	HTTP Doer
}

// Account is where and as whom to call listmonk.
type Account struct {
	BaseURL string // https, without a trailing slash (validated with the block)
	User    string // API user name
	Token   string // API user's access token
}

const maxBody = 256 << 10

// CheckList confirms that the account works and the list exists.
func (c *Client) CheckList(ctx context.Context, a Account, listID int64) error {
	status, body, err := c.call(ctx, a, http.MethodGet, "/api/lists/"+strconv.FormatInt(listID, 10), nil)
	if err != nil {
		return err
	}
	if status == http.StatusNotFound {
		return ErrListNotFound
	}
	return statusErr(status, body)
}

type subscriber struct {
	ID     int64  `json:"id"`
	Status string `json:"status"` // enabled, disabled, blocklisted
	Lists  []struct {
		ID                 int64  `json:"id"`
		SubscriptionStatus string `json:"subscription_status"` // unconfirmed, confirmed, unsubscribed
	} `json:"lists"`
}

// Subscribe adds email to the list as a confirmed subscriber; the visitor
// already confirmed through Kork's double opt-in. It is safe to repeat: an
// address already on the list, unsubscribed from it or blocklisted is left
// as it is.
func (c *Client) Subscribe(ctx context.Context, a Account, listID int64, email string) (Outcome, error) {
	create := map[string]any{
		"email": email, "name": nameFor(email), "status": "enabled",
		"lists": []int64{listID}, "preconfirm_subscriptions": true,
	}
	status, body, err := c.call(ctx, a, http.MethodPost, "/api/subscribers", create)
	if err != nil {
		return "", err
	}
	if status != http.StatusConflict {
		if err := statusErr(status, body); err != nil {
			return "", err
		}
		return Added, nil
	}
	// The address exists in that listmonk. Look at it before changing
	// anything, so an unsubscribe or blocklist is never reversed.
	sub, err := c.find(ctx, a, email)
	if err != nil {
		return "", err
	}
	if sub.Status == "blocklisted" {
		return Blocklisted, nil
	}
	for _, l := range sub.Lists {
		if l.ID != listID {
			continue
		}
		if l.SubscriptionStatus == "unsubscribed" {
			return Unsubscribed, nil
		}
		return AlreadyOnList, nil
	}
	add := map[string]any{"ids": []int64{sub.ID}, "action": "add", "target_list_ids": []int64{listID}, "status": "confirmed"}
	status, body, err = c.call(ctx, a, http.MethodPut, "/api/subscribers/lists", add)
	if err != nil {
		return "", err
	}
	if err := statusErr(status, body); err != nil {
		return "", err
	}
	return Added, nil
}

// find looks up one subscriber by email. listmonk's query parameter is an
// SQL expression, so the address is passed as a quoted string literal.
func (c *Client) find(ctx context.Context, a Account, email string) (*subscriber, error) {
	q := url.Values{}
	q.Set("query", "subscribers.email = '"+strings.ReplaceAll(strings.ToLower(email), "'", "''")+"'")
	q.Set("per_page", "1")
	status, body, err := c.call(ctx, a, http.MethodGet, "/api/subscribers?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	if err := statusErr(status, body); err != nil {
		return nil, err
	}
	var out struct {
		Data struct {
			Results []subscriber `json:"results"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("listmonk: reading subscriber: %w", err)
	}
	if len(out.Data.Results) == 0 {
		// listmonk said the address exists but can't find it; retry later.
		return nil, errors.New("listmonk: existing subscriber not found")
	}
	return &out.Data.Results[0], nil
}

func (c *Client) call(ctx context.Context, a Account, method, path string, body any) (int, []byte, error) {
	var rd *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		rd = bytes.NewReader(b)
	}
	var req *http.Request
	var err error
	if rd != nil {
		req, err = http.NewRequestWithContext(ctx, method, a.BaseURL+path, rd)
	} else {
		req, err = http.NewRequestWithContext(ctx, method, a.BaseURL+path, nil)
	}
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "token "+a.User+":"+a.Token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	status, data, err := c.HTTP.Do(req, maxBody)
	if err == nil && (status == http.StatusUnauthorized || status == http.StatusForbidden) {
		return 0, nil, ErrUnauthorized
	}
	return status, data, err
}

func statusErr(status int, body []byte) error {
	if status >= 200 && status < 300 {
		return nil
	}
	var e struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &e)
	if e.Message == "" {
		e.Message = http.StatusText(status)
	}
	return &APIError{Status: status, Message: e.Message}
}

// nameFor gives listmonk the address's local part as the subscriber name,
// which older listmonk versions require.
func nameFor(email string) string {
	if i := strings.IndexByte(email, '@'); i > 0 {
		return email[:i]
	}
	return email
}
