package downloads

import (
	"strconv"
	"testing"
	"time"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func TestSelectionsBelongToTheirPersonAndExpire(t *testing.T) {
	c := &clock{time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)}
	s := &Store{Now: c.now}
	sel := s.Add("maria", []string{"a", "b"}, "Share 2026-09-27.zip", false)
	if got, ok := s.Get(sel.ID, "maria"); !ok || got.Name != "Share 2026-09-27.zip" || len(got.FileIDs) != 2 {
		t.Fatalf("Get: %+v %v", got, ok)
	}
	if _, ok := s.Get(sel.ID, "peter"); ok {
		t.Error("someone else got Maria's selection")
	}
	c.t = c.t.Add(Keep - time.Minute)
	if _, ok := s.Get(sel.ID, "maria"); !ok {
		t.Error("gone before a day without use")
	}
	c.t = c.t.Add(Keep - time.Minute) // used a minute ago, so it lasts
	if _, ok := s.Get(sel.ID, "maria"); !ok {
		t.Error("using it didn't keep it")
	}
	c.t = c.t.Add(Keep)
	s.Prune()
	if _, ok := s.Get(sel.ID, "maria"); ok {
		t.Error("still there a day after its last use")
	}
}

func TestTooManySelectionsDropTheOldest(t *testing.T) {
	c := &clock{time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)}
	s := &Store{Now: c.now}
	var first []string
	for i := range perPerson + 2 {
		c.t = c.t.Add(time.Second)
		sel := s.Add("maria", nil, strconv.Itoa(i), false)
		if i < 2 {
			first = append(first, sel.ID)
		}
	}
	for _, id := range first {
		if _, ok := s.Get(id, "maria"); ok {
			t.Errorf("an old selection %s is still there", id)
		}
	}
	for i := range total {
		c.t = c.t.Add(time.Second)
		s.Add("user"+strconv.Itoa(i), nil, "", false)
	}
	if n := len(s.sel); n != total {
		t.Errorf("%d selections, want at most %d", n, total)
	}
}

func TestStreamsAreLimited(t *testing.T) {
	s := &Store{Now: time.Now}
	a, ok1 := s.Stream("maria")
	_, ok2 := s.Stream("maria")
	if _, ok := s.Stream("maria"); !ok1 || !ok2 || ok {
		t.Fatal("Maria got a third stream at once")
	}
	if _, ok := s.Stream("peter"); !ok {
		t.Fatal("Peter got none")
	}
	if _, ok := s.Stream("rosa"); !ok {
		t.Fatal("Rosa got none")
	}
	if _, ok := s.Stream("oma"); ok {
		t.Fatal("a fifth stream in all")
	}
	a()
	a() // releasing twice counts once
	if _, ok := s.Stream("maria"); !ok {
		t.Error("Maria's released slot is still taken")
	}
	if _, ok := s.Stream("oma"); ok {
		t.Error("releasing twice freed two slots")
	}
}
