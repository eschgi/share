package upload

import (
	"testing"
	"time"
)

// The date taken counts on its day in the server's time zone, and never for an encrypted file.
func TestTakenAt(t *testing.T) {
	rome, _ := time.LoadLocation("Europe/Rome")
	now := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	ms := time.Date(2026, 9, 26, 22, 30, 0, 0, time.UTC).UnixMilli() // 00:30 on the 27th in Rome
	if at, day := takenAt(ms, now, rome, false); at == nil || at.UnixMilli() != ms || day != "2026-09-27" {
		t.Errorf("takenAt = %v, %q", at, day)
	}
	if at, day := takenAt(ms, now, rome, true); at != nil || day != "" {
		t.Errorf("an encrypted file kept %v, %q", at, day)
	}
	old := time.Date(1962, 5, 1, 12, 0, 0, 0, time.UTC).UnixMilli() // a scanned photo, before 1970
	if at, day := takenAt(old, now, rome, false); at == nil || day != "1962-05-01" {
		t.Errorf("1962: %v, %q", at, day)
	}
	for _, bad := range []int64{0, time.Date(1850, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli(), now.Add(72 * time.Hour).UnixMilli()} {
		if at, _ := takenAt(bad, now, rome, false); at != nil {
			t.Errorf("takenAt(%d) = %v", bad, at)
		}
	}
}
