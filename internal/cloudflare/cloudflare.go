// Package cloudflare is a small client for the Cloudflare for SaaS custom
// hostnames API. It talks only to Cloudflare's fixed API host, so it uses a
// plain net/http client rather than the SSRF-checked fetcher meant for
// user-supplied URLs.
package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultBaseURL is Cloudflare's v4 API.
const DefaultBaseURL = "https://api.cloudflare.com/client/v4"

// maxResponse caps how much of a response body is read.
const maxResponse = 1 << 20

// ErrNotFound means Cloudflare has no custom hostname with that ID (HTTP 404).
var ErrNotFound = errors.New("cloudflare: custom hostname not found")

// Client calls the custom hostnames API for one zone.
type Client struct {
	BaseURL string
	Token   string
	ZoneID  string
	HTTP    *http.Client
}

// New returns a client for zoneID using an API token with
// "SSL and Certificates: Edit" on that zone.
func New(token, zoneID string) *Client {
	return &Client{BaseURL: DefaultBaseURL, Token: token, ZoneID: zoneID, HTTP: &http.Client{Timeout: 15 * time.Second}}
}

// Record is a DNS record the customer may need to add.
type Record struct {
	Type  string `json:"type"`
	Name  string `json:"name"`
	Value string `json:"value"`
}

// ValidationRecord is one certificate validation record from ssl.validation_records.
type ValidationRecord struct {
	Status      string `json:"status,omitempty"`
	TXTName     string `json:"txt_name,omitempty"`
	TXTValue    string `json:"txt_value,omitempty"`
	HTTPURL     string `json:"http_url,omitempty"`
	HTTPBody    string `json:"http_body,omitempty"`
	CNAME       string `json:"cname,omitempty"`
	CNAMETarget string `json:"cname_target,omitempty"`
}

// SSL is the certificate state of a custom hostname.
type SSL struct {
	Status            string             `json:"status"`
	Method            string             `json:"method"`
	Type              string             `json:"type"`
	ValidationRecords []ValidationRecord `json:"validation_records"`
	ValidationErrors  []struct {
		Message string `json:"message"`
	} `json:"validation_errors"`
}

// Hostname is a custom hostname as Cloudflare reports it.
type Hostname struct {
	ID                    string   `json:"id"`
	Hostname              string   `json:"hostname"`
	Status                string   `json:"status"`
	SSL                   SSL      `json:"ssl"`
	VerificationErrors    []string `json:"verification_errors"`
	OwnershipVerification *Record  `json:"ownership_verification"`
}

// Active reports whether Cloudflare routes and serves HTTPS for the hostname.
func (h *Hostname) Active() bool { return h.Status == "active" && h.SSL.Status == "active" }

// Errors returns every human-readable problem Cloudflare reported.
func (h *Hostname) Errors() []string {
	out := []string{}
	for _, e := range h.VerificationErrors {
		if e = strings.TrimSpace(e); e != "" {
			out = append(out, e)
		}
	}
	for _, e := range h.SSL.ValidationErrors {
		if m := strings.TrimSpace(e.Message); m != "" {
			out = append(out, m)
		}
	}
	return out
}

// APIError is a non-2xx or success:false response.
type APIError struct {
	Status   int
	Codes    []int
	Messages []string
}

func (e *APIError) Error() string {
	msg := strings.Join(e.Messages, "; ")
	if msg == "" {
		msg = http.StatusText(e.Status)
	}
	return fmt.Sprintf("cloudflare: %s (HTTP %d)", msg, e.Status)
}

// Message returns Cloudflare's messages without the status prefix, for users.
func (e *APIError) Message() string {
	if len(e.Messages) == 0 {
		return "Cloudflare returned HTTP " + fmt.Sprint(e.Status)
	}
	return strings.Join(e.Messages, "; ")
}

// HasCode reports whether Cloudflare returned the given error code.
func (e *APIError) HasCode(code int) bool {
	for _, c := range e.Codes {
		if c == code {
			return true
		}
	}
	return false
}

// CodeDuplicate is Cloudflare's "Duplicate custom hostname found" error.
const CodeDuplicate = 1406

// Create adds hostname with an HTTP-validated DV certificate.
func (c *Client) Create(ctx context.Context, hostname string) (*Hostname, error) {
	body := map[string]any{"hostname": hostname, "ssl": map[string]string{"method": "http", "type": "dv"}}
	var h Hostname
	if err := c.do(ctx, http.MethodPost, c.path(""), body, &h); err != nil {
		return nil, err
	}
	return &h, nil
}

// Get returns the current state of a custom hostname.
func (c *Client) Get(ctx context.Context, id string) (*Hostname, error) {
	var h Hostname
	if err := c.do(ctx, http.MethodGet, c.path(id), nil, &h); err != nil {
		return nil, err
	}
	return &h, nil
}

// Find returns the custom hostname with exactly this name, or ErrNotFound.
func (c *Client) Find(ctx context.Context, hostname string) (*Hostname, error) {
	var list []Hostname
	if err := c.do(ctx, http.MethodGet, c.path("")+"?hostname="+url.QueryEscape(hostname), nil, &list); err != nil {
		return nil, err
	}
	for i := range list {
		if strings.EqualFold(list[i].Hostname, hostname) {
			return &list[i], nil
		}
	}
	return nil, ErrNotFound
}

// Delete removes a custom hostname. It returns ErrNotFound on 404.
func (c *Client) Delete(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, c.path(id), nil, nil)
}

func (c *Client) path(id string) string {
	p := strings.TrimSuffix(c.BaseURL, "/") + "/zones/" + url.PathEscape(c.ZoneID) + "/custom_hostnames"
	if id != "" {
		p += "/" + url.PathEscape(id)
	}
	return p
}

type envelope struct {
	Success bool `json:"success"`
	Errors  []struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
	Result json.RawMessage `json:"result"`
}

func (c *Client) do(ctx context.Context, method, u string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}
	res, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("cloudflare: %w", err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, maxResponse))
	if err != nil {
		return fmt.Errorf("cloudflare: read response: %w", err)
	}
	var env envelope
	jsonErr := json.Unmarshal(data, &env)
	if res.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if res.StatusCode < 200 || res.StatusCode > 299 || jsonErr != nil || !env.Success {
		ae := &APIError{Status: res.StatusCode}
		for _, e := range env.Errors {
			ae.Codes = append(ae.Codes, e.Code)
			ae.Messages = append(ae.Messages, e.Message)
		}
		if len(ae.Messages) == 0 && jsonErr != nil {
			ae.Messages = []string{"unexpected response from Cloudflare"}
		}
		return ae
	}
	if out != nil && len(env.Result) > 0 && string(env.Result) != "null" {
		if err := json.Unmarshal(env.Result, out); err != nil {
			return fmt.Errorf("cloudflare: decode result: %w", err)
		}
	}
	return nil
}
