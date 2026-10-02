// Package brevo adds contacts to a Brevo list with a creator's own API key.
package brevo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// DefaultBaseURL is Brevo's API origin.
const DefaultBaseURL = "https://api.brevo.com"

var (
	// ErrUnauthorized means Brevo rejected the API key.
	ErrUnauthorized = errors.New("Brevo rejected the API key")
	// ErrListNotFound means the list ID does not exist in that account.
	ErrListNotFound = errors.New("Brevo has no list with that ID")
)

// Client calls the Brevo API. BaseURL is replaced in tests.
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

// New returns a client for the real Brevo API.
func New() *Client {
	return &Client{BaseURL: DefaultBaseURL, HTTP: &http.Client{Timeout: 10 * time.Second}}
}

// CheckList confirms that the key works and the list exists.
func (c *Client) CheckList(ctx context.Context, apiKey string, listID int64) error {
	_, err := c.do(ctx, apiKey, http.MethodGet, "/v3/contacts/lists/"+strconv.FormatInt(listID, 10), nil)
	return err
}

// AddContact adds email to the list, creating the contact if needed. An
// existing contact is updated rather than reported as a conflict.
func (c *Client) AddContact(ctx context.Context, apiKey string, listID int64, email string) error {
	body := map[string]any{"email": email, "listIds": []int64{listID}, "updateEnabled": true}
	_, err := c.do(ctx, apiKey, http.MethodPost, "/v3/contacts", body)
	return err
}

func (c *Client) do(ctx context.Context, apiKey, method, path string, body any) ([]byte, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimSuffix(c.BaseURL, "/")+path, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("api-key", apiKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	switch {
	case res.StatusCode >= 200 && res.StatusCode < 300:
		return data, nil
	case res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden:
		return nil, ErrUnauthorized
	case res.StatusCode == http.StatusNotFound:
		return nil, ErrListNotFound
	}
	var e struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(data, &e)
	if e.Message != "" {
		return nil, fmt.Errorf("brevo: %d %s: %s", res.StatusCode, e.Code, e.Message)
	}
	return nil, fmt.Errorf("brevo: unexpected status %d", res.StatusCode)
}
