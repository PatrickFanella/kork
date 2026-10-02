package brevo

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient(t *testing.T) {
	var added []map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("api-key") != "good-key" {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"code":"unauthorized","message":"Key not found"}`))
			return
		}
		switch {
		case r.Method == "GET" && r.URL.Path == "/v3/contacts/lists/7":
			w.Write([]byte(`{"id":7,"name":"Newsletter"}`))
		case r.Method == "GET":
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"code":"document_not_found","message":"List ID does not exist"}`))
		case r.Method == "POST" && r.URL.Path == "/v3/contacts":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
			if body["email"] == "bad" {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(`{"code":"invalid_parameter","message":"email is not valid"}`))
				return
			}
			added = append(added, body)
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"id":1}`))
		}
	}))
	defer ts.Close()
	c := &Client{BaseURL: ts.URL, HTTP: ts.Client()}
	ctx := context.Background()

	if err := c.CheckList(ctx, "good-key", 7); err != nil {
		t.Fatalf("CheckList: %v", err)
	}
	if err := c.CheckList(ctx, "bad-key", 7); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("bad key: %v", err)
	}
	if err := c.CheckList(ctx, "good-key", 8); !errors.Is(err, ErrListNotFound) {
		t.Errorf("missing list: %v", err)
	}
	if err := c.AddContact(ctx, "good-key", 7, "fan@example.org"); err != nil {
		t.Fatalf("AddContact: %v", err)
	}
	if len(added) != 1 || added[0]["email"] != "fan@example.org" || added[0]["updateEnabled"] != true {
		t.Fatalf("added: %+v", added)
	}
	if ids, _ := added[0]["listIds"].([]any); len(ids) != 1 || ids[0] != float64(7) {
		t.Errorf("listIds: %+v", added[0]["listIds"])
	}
	if err := c.AddContact(ctx, "good-key", 7, "bad"); err == nil || err.Error() != "brevo: 400 invalid_parameter: email is not valid" {
		t.Errorf("bad email: %v", err)
	}
}
