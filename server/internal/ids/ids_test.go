package ids

import (
	"bytes"
	"strconv"
	"testing"
	"time"
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

// An id is a UUID version 7: the time it was made, the version, RFC 9562's variant and random
// bits, so a later id sorts after an earlier one.
func TestNewIsAUUIDv7(t *testing.T) {
	before := time.Now().UnixMilli()
	id := New()
	after := time.Now().UnixMilli()
	ms, err := strconv.ParseInt(id[0:8]+id[9:13], 16, 64)
	if err != nil || ms < before || ms > after {
		t.Errorf("%s holds the time %d, want %d to %d", id, ms, before, after)
	}
	if id[14] != '7' || !bytes.ContainsRune([]byte("89ab"), rune(id[19])) {
		t.Errorf("%s: version %c, variant %c", id, id[14], id[19])
	}
	time.Sleep(2 * time.Millisecond)
	if later := New(); later <= id {
		t.Errorf("%s, made later, sorts before %s", later, id)
	}
}

func TestValid(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want bool
	}{
		{"0199b3a4-6f2e-7c41-9d3a-5e8f0b2c4d6e", true},
		{"00000000-0000-0000-0000-000000000000", true},
		{"0199B3A4-6F2E-7C41-9D3A-5E8F0B2C4D6E", false}, // upper case
		{"0199b3a46f2e7c419d3a5e8f0b2c4d6e", false},     // without dashes
		{"0199b3a4-6f2e-7c41-9d3a-5e8f0b2c4d6", false},  // a digit short
		{"0199b3a4-6f2e-7c41-9d3a-5e8f0b2c4d6ex", false},
		{"abcdefghijklmnopqrstuvwxyz", false}, // the base32 ids of before
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
