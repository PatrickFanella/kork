// Package cftest is an in-memory fake of the Cloudflare custom hostnames API
// for tests. It never contacts Cloudflare.
package cftest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"git.subcult.tv/subculture-collective/zelda/internal/cloudflare"
)

const (
	Token  = "test-token"
	ZoneID = "zone123"
)

// API is a fake Cloudflare API serving one zone.
type API struct {
	*httptest.Server

	mu        sync.Mutex
	next      int
	hostnames map[string]*cloudflare.Hostname // by ID
	// FailCreate, when set, makes POST return this HTTP status and message.
	FailCreate     int
	FailCreateMsg  string
	FailDelete     int
	Calls          []string // "METHOD path" for each request
	OwnershipTXT   bool     // include an ownership_verification TXT record on create
	ValidationTXTs bool     // include a TXT ssl.validation_record on create
}

// New starts a fake API and closes it when the test ends.
func New(t testing.TB) *API {
	a := &API{hostnames: map[string]*cloudflare.Hostname{}}
	a.Server = httptest.NewServer(http.HandlerFunc(a.serve))
	t.Cleanup(a.Close)
	return a
}

// Client returns a client pointed at the fake.
func (a *API) Client() *cloudflare.Client {
	c := cloudflare.New(Token, ZoneID)
	c.BaseURL = a.URL + "/client/v4"
	c.HTTP = a.Server.Client()
	return c
}

// Set changes the stored state of hostname (by name) as Cloudflare would.
func (a *API) Set(hostname, status, sslStatus string, verificationErrors ...string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, h := range a.hostnames {
		if h.Hostname == hostname {
			h.Status, h.SSL.Status, h.VerificationErrors = status, sslStatus, verificationErrors
			if sslStatus == "active" {
				h.SSL.ValidationRecords = nil
			}
			return
		}
	}
	panic("cftest: unknown hostname " + hostname)
}

// Remove deletes a hostname as if Cloudflare had dropped it.
func (a *API) Remove(hostname string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for id, h := range a.hostnames {
		if h.Hostname == hostname {
			delete(a.hostnames, id)
		}
	}
}

// Has reports whether a hostname exists.
func (a *API) Has(hostname string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, h := range a.hostnames {
		if h.Hostname == hostname {
			return true
		}
	}
	return false
}

// Count returns how many requests matched "METHOD" (and optional path prefix).
func (a *API) Count(method string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	n := 0
	for _, c := range a.Calls {
		if strings.HasPrefix(c, method+" ") {
			n++
		}
	}
	return n
}

func (a *API) serve(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.Calls = append(a.Calls, r.Method+" "+r.URL.Path)
	if r.Header.Get("Authorization") != "Bearer "+Token {
		reply(w, http.StatusForbidden, nil, 10000, "Authentication error")
		return
	}
	prefix := "/client/v4/zones/" + ZoneID + "/custom_hostnames"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		reply(w, http.StatusNotFound, nil, 7003, "Could not route to "+r.URL.Path)
		return
	}
	id := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, prefix), "/")
	switch {
	case r.Method == http.MethodPost && id == "":
		a.create(w, r)
	case r.Method == http.MethodGet && id == "":
		out := []*cloudflare.Hostname{}
		for _, h := range a.hostnames {
			if h.Hostname == r.URL.Query().Get("hostname") {
				out = append(out, h)
			}
		}
		reply(w, http.StatusOK, out, 0, "")
	case r.Method == http.MethodGet:
		if h, ok := a.hostnames[id]; ok {
			reply(w, http.StatusOK, h, 0, "")
		} else {
			reply(w, http.StatusNotFound, nil, 1436, "The custom hostname was not found.")
		}
	case r.Method == http.MethodDelete:
		if a.FailDelete != 0 {
			reply(w, a.FailDelete, nil, 1000, "Internal error")
			return
		}
		if _, ok := a.hostnames[id]; !ok {
			reply(w, http.StatusNotFound, nil, 1436, "The custom hostname was not found.")
			return
		}
		delete(a.hostnames, id)
		reply(w, http.StatusOK, map[string]string{"id": id}, 0, "")
	default:
		reply(w, http.StatusMethodNotAllowed, nil, 10000, "method not allowed")
	}
}

func (a *API) create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Hostname string
		SSL      struct{ Method, Type string }
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Hostname == "" {
		reply(w, http.StatusBadRequest, nil, 1400, "Bad request")
		return
	}
	if body.SSL.Method != "http" || body.SSL.Type != "dv" {
		reply(w, http.StatusBadRequest, nil, 1401, "ssl.method must be http and ssl.type dv")
		return
	}
	if a.FailCreate != 0 {
		reply(w, a.FailCreate, nil, 1414, a.FailCreateMsg)
		return
	}
	for _, h := range a.hostnames {
		if h.Hostname == body.Hostname {
			reply(w, http.StatusConflict, nil, cloudflare.CodeDuplicate, "Duplicate custom hostname found.")
			return
		}
	}
	a.next++
	h := &cloudflare.Hostname{
		ID: fmt.Sprintf("cfh-%04d", a.next), Hostname: body.Hostname, Status: "pending",
		SSL:                cloudflare.SSL{Status: "initializing", Method: "http", Type: "dv"},
		VerificationErrors: []string{"custom hostname does not CNAME to this zone."},
	}
	if a.OwnershipTXT {
		h.OwnershipVerification = &cloudflare.Record{Type: "txt", Name: "_cf-custom-hostname." + body.Hostname, Value: "0e2d5a7f-1b3c"}
	}
	if a.ValidationTXTs {
		h.SSL.Status = "pending_validation"
		h.SSL.ValidationRecords = []cloudflare.ValidationRecord{
			{Status: "pending", TXTName: "_acme-challenge." + body.Hostname, TXTValue: "ca3-abc"},
			{Status: "pending", HTTPURL: "http://" + body.Hostname + "/.well-known/pki-validation/x.txt", HTTPBody: "ca3-abc"},
		}
	}
	a.hostnames[h.ID] = h
	reply(w, http.StatusOK, h, 0, "")
}

func reply(w http.ResponseWriter, status int, result any, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	env := map[string]any{"success": status < 300, "errors": []any{}, "messages": []any{}, "result": result}
	if code != 0 {
		env["errors"] = []map[string]any{{"code": code, "message": msg}}
	}
	_ = json.NewEncoder(w).Encode(env)
}
