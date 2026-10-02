package seal

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"testing"
)

func TestParseKey(t *testing.T) {
	key := bytes.Repeat([]byte{0xfb}, KeySize)
	for _, s := range []string{
		base64.StdEncoding.EncodeToString(key),
		base64.RawStdEncoding.EncodeToString(key),
		base64.URLEncoding.EncodeToString(key),
		base64.RawURLEncoding.EncodeToString(key),
		hex.EncodeToString(key),
	} {
		got, err := ParseKey(s)
		if err != nil || !bytes.Equal(got, key) {
			t.Errorf("ParseKey(%q) = %x, %v", s, got, err)
		}
	}
	for _, s := range []string{"", "short", base64.StdEncoding.EncodeToString(key[:16]), hex.EncodeToString(key[:31])} {
		if _, err := ParseKey(s); err == nil {
			t.Errorf("ParseKey(%q) accepted", s)
		}
	}
}

func TestSealOpen(t *testing.T) {
	s, err := New(bytes.Repeat([]byte{1}, KeySize))
	if err != nil {
		t.Fatal(err)
	}
	box := s.Seal([]byte("refresh-token"), []byte("user-1"))
	if bytes.Contains(box, []byte("refresh-token")) {
		t.Fatal("plaintext visible in box")
	}
	got, err := s.Open(box, []byte("user-1"))
	if err != nil || string(got) != "refresh-token" {
		t.Fatalf("open: %q %v", got, err)
	}
	if _, err := s.Open(box, []byte("user-2")); err == nil {
		t.Fatal("opened with another aad")
	}
	box[len(box)-1] ^= 1
	if _, err := s.Open(box, []byte("user-1")); err == nil {
		t.Fatal("opened a modified box")
	}
	if _, err := s.Open([]byte{1, 2}, nil); err == nil {
		t.Fatal("opened a short box")
	}
	other, _ := New(bytes.Repeat([]byte{2}, KeySize))
	if _, err := other.Open(s.Seal([]byte("x"), nil), nil); err == nil {
		t.Fatal("opened under another key")
	}
	if _, err := New([]byte("short")); err == nil {
		t.Fatal("short key accepted")
	}
}
