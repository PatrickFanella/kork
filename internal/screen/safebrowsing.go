package screen

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// SafeBrowsingEndpoint is the Google Safe Browsing Lookup API v4 URL.
const SafeBrowsingEndpoint = "https://safebrowsing.googleapis.com/v4/threatMatches:find"

// SafeBrowsing checks URLs with the Google Safe Browsing Lookup API v4.
type SafeBrowsing struct {
	Key string
	// Endpoint defaults to SafeBrowsingEndpoint.
	Endpoint string
	// Client defaults to SafeClient(15s).
	Client *http.Client
}

// NewSafeBrowsing returns a Safe Browsing screener for key.
func NewSafeBrowsing(key string) *SafeBrowsing {
	return &SafeBrowsing{Key: key, Endpoint: SafeBrowsingEndpoint, Client: SafeClient(15 * time.Second)}
}

type sbEntry struct {
	URL string `json:"url"`
}

type sbRequest struct {
	Client struct {
		ClientID      string `json:"clientId"`
		ClientVersion string `json:"clientVersion"`
	} `json:"client"`
	ThreatInfo struct {
		ThreatTypes      []string  `json:"threatTypes"`
		PlatformTypes    []string  `json:"platformTypes"`
		ThreatEntryTypes []string  `json:"threatEntryTypes"`
		ThreatEntries    []sbEntry `json:"threatEntries"`
	} `json:"threatInfo"`
}

type sbResponse struct {
	Matches []struct {
		ThreatType string  `json:"threatType"`
		Threat     sbEntry `json:"threat"`
	} `json:"matches"`
}

// sbBatch is the API's limit on entries per request.
const sbBatch = 500

func (s *SafeBrowsing) Check(ctx context.Context, urls []string) (map[string]Verdict, error) {
	out := make(map[string]Verdict, len(urls))
	for i := 0; i < len(urls); i += sbBatch {
		batch := urls[i:min(i+sbBatch, len(urls))]
		if err := s.check(ctx, batch, out); err != nil {
			return out, err
		}
	}
	return out, nil
}

func (s *SafeBrowsing) check(ctx context.Context, urls []string, out map[string]Verdict) error {
	var req sbRequest
	req.Client.ClientID = "kork"
	req.Client.ClientVersion = "1"
	req.ThreatInfo.ThreatTypes = []string{"MALWARE", "SOCIAL_ENGINEERING", "UNWANTED_SOFTWARE", "POTENTIALLY_HARMFUL_APPLICATION"}
	req.ThreatInfo.PlatformTypes = []string{"ANY_PLATFORM"}
	req.ThreatInfo.ThreatEntryTypes = []string{"URL"}
	for _, u := range urls {
		req.ThreatInfo.ThreatEntries = append(req.ThreatInfo.ThreatEntries, sbEntry{u})
	}
	body, _ := json.Marshal(req)
	endpoint := s.Endpoint
	if endpoint == "" {
		endpoint = SafeBrowsingEndpoint
	}
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"?key="+url.QueryEscape(s.Key), bytes.NewReader(body))
	if err != nil {
		return err
	}
	hr.Header.Set("Content-Type", "application/json")
	client := s.Client
	if client == nil {
		client = SafeClient(15 * time.Second)
	}
	res, err := client.Do(hr)
	if err != nil {
		// The error text includes the request URL; keep the key out of logs.
		return fmt.Errorf("safe browsing: request failed")
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return fmt.Errorf("safe browsing: read response: %w", err)
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("safe browsing: HTTP %d", res.StatusCode)
	}
	var sr sbResponse
	if len(bytes.TrimSpace(data)) > 0 {
		if err := json.Unmarshal(data, &sr); err != nil {
			return fmt.Errorf("safe browsing: decode response: %w", err)
		}
	}
	for _, u := range urls {
		out[u] = Verdict{}
	}
	for _, m := range sr.Matches {
		if _, asked := out[m.Threat.URL]; asked {
			out[m.Threat.URL] = Verdict{Unsafe: true, Threat: m.ThreatType, Source: "Google Safe Browsing"}
		}
	}
	return nil
}
