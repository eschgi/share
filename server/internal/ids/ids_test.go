package ids

import (
	"bytes"
	"testing"
)

func TestNewIsValidAndUnique(t *testing.T) {
	seen := map[string]bool{}
	for range 1000 {
		id := New()
		if !Valid(id) {
			t.Fatalf("New() = %q, not a valid id", id)
		}
		if seen[id] {
			t.Fatalf("New() repeated %q", id)
		}
		seen[id] = true
	}
}

func TestValid(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want bool
	}{
		{"abcdefghijklmnopqrstuvwxyz", true},
		{"234567234567234567234567ab", true},
		{"ABCDEFGHIJKLMNOPQRSTUVWXYZ", false}, // upper case
		{"abcdefghijklmnopqrstuvwxy", false},  // 25 characters
		{"abcdefghijklmnopqrstuvwxyz2", false},
		{"abcdefghijklmnopqrstuvwxy1", false}, // 1 is not in the alphabet
		{"../../etc/passwd", false},
		{"", false},
	} {
		if got := Valid(tc.in); got != tc.want {
			t.Errorf("Valid(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestTokens(t *testing.T) {
	token, hash := NewToken(PrefixPinSession)
	if !TokenHasPrefix(token, PrefixPinSession) {
		t.Fatalf("token %q does not match its own prefix", token)
	}
	if TokenHasPrefix(token, PrefixDevice) {
		t.Fatalf("token %q matches the wrong prefix", token)
	}
	if !bytes.Equal(hash, HashToken(token)) {
		t.Fatal("hash differs from HashToken(token)")
	}
	for _, bad := range []string{"", "shp_", token + "x", token[:len(token)-1], "shp_" + string(make([]byte, 43))} {
		if TokenHasPrefix(bad, PrefixPinSession) {
			t.Errorf("TokenHasPrefix(%q) = true", bad)
		}
	}
}
