package ratelimit

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)

func at(sec int) time.Time { return t0.Add(time.Duration(sec) * time.Second) }

func TestBlocksAfterMaxFailuresAndUnblocksLater(t *testing.T) {
	l := New(5, 10*time.Minute, 10*time.Minute)
	for i := 1; i <= 4; i++ {
		left, blocked := l.Fail("phone", at(i))
		if left != 5-i || blocked != 0 {
			t.Fatalf("failure %d: left %d, blocked %v", i, left, blocked)
		}
	}
	if left, blocked := l.Fail("phone", at(5)); left != 0 || blocked != 10*time.Minute {
		t.Fatalf("5th failure: left %d, blocked %v", left, blocked)
	}
	if b, retry := l.Check("phone", at(65)); !b || retry != 10*time.Minute-60*time.Second {
		t.Fatalf("Check during block = %v, %v", b, retry)
	}
	if b, _ := l.Check("other", at(65)); b {
		t.Fatal("an unrelated key is blocked")
	}
	if b, _ := l.Check("phone", at(5+600)); b {
		t.Fatal("still blocked after the block ended")
	}
	if left, _ := l.Fail("phone", at(5+600)); left != 4 {
		t.Fatalf("after the block, failures start over: left %d, want 4", left)
	}
}

func TestFailuresOutsideTheWindowStartOver(t *testing.T) {
	l := New(3, time.Minute, time.Hour)
	l.Fail("k", at(0))
	l.Fail("k", at(30))
	if left, _ := l.Fail("k", at(61)); left != 2 {
		t.Fatalf("a failure after the window counts as the first: left %d, want 2", left)
	}
}

func TestResetAndPrune(t *testing.T) {
	l := New(3, time.Minute, time.Hour)
	l.Fail("a", at(0))
	l.Reset("a")
	if left, _ := l.Fail("a", at(1)); left != 2 {
		t.Fatalf("after Reset: left %d, want 2", left)
	}
	l.Fail("b", at(0))
	l.Fail("b", at(0))
	l.Fail("b", at(0)) // blocked for an hour
	l.Prune(at(120))
	if l.Len() != 1 {
		t.Fatalf("Prune kept %d keys, want only the blocked one", l.Len())
	}
	l.Prune(at(3601))
	if l.Len() != 0 {
		t.Fatalf("Prune kept %d keys after the block ended", l.Len())
	}
}
