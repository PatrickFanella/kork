package seal

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"testing"
)

func testKey() []byte { return bytes.Repeat([]byte{7}, KeySize) }

func TestParseKey(t *testing.T) {
	key := testKey()
	for _, in := range []string{
		base64.StdEncoding.EncodeToString(key),
		base64.RawURLEncoding.EncodeToString(key),
		hex.EncodeToString(key),
		"  " + base64.StdEncoding.EncodeToString(key) + "\n",
	} {
		got, err := ParseKey(in)
		if err != nil || !bytes.Equal(got, key) {
			t.Errorf("ParseKey(%q) = %x, %v", in, got, err)
		}
	}
	for _, in := range []string{"", "short", base64.StdEncoding.EncodeToString(key[:16]), hex.EncodeToString(key)[:62]} {
		if _, err := ParseKey(in); !errors.Is(err, ErrKey) {
			t.Errorf("ParseKey(%q): want ErrKey, got %v", in, err)
		}
	}
}

func TestSealOpen(t *testing.T) {
	s, err := New(testKey())
	if err != nil {
		t.Fatal(err)
	}
	box := s.Seal([]byte("xkeysib-secret"), []byte("block-1"))
	if bytes.Contains(box, []byte("xkeysib")) {
		t.Fatal("plaintext visible in sealed value")
	}
	if again := s.Seal([]byte("xkeysib-secret"), []byte("block-1")); bytes.Equal(again, box) {
		t.Fatal("nonce reused")
	}
	pt, err := s.Open(box, []byte("block-1"))
	if err != nil || string(pt) != "xkeysib-secret" {
		t.Fatalf("Open = %q, %v", pt, err)
	}
	if _, err := s.Open(box, []byte("block-2")); !errors.Is(err, ErrOpen) {
		t.Errorf("other aad: %v", err)
	}
	other, _ := New(bytes.Repeat([]byte{8}, KeySize))
	if _, err := other.Open(box, []byte("block-1")); !errors.Is(err, ErrOpen) {
		t.Errorf("other key: %v", err)
	}
	box[len(box)-1] ^= 1
	if _, err := s.Open(box, []byte("block-1")); !errors.Is(err, ErrOpen) {
		t.Errorf("tampered: %v", err)
	}
	if _, err := s.Open([]byte{1, 2, 3}, nil); !errors.Is(err, ErrOpen) {
		t.Errorf("short: %v", err)
	}
	if _, err := New([]byte("short")); !errors.Is(err, ErrKey) {
		t.Errorf("New short key: %v", err)
	}
}
