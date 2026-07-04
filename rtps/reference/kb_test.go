package reference

import (
	"context"
	"testing"
)

// TestStaticKB covers registration + lookup + unknown handling.
func TestStaticKB(t *testing.T) {
	ctx := context.Background()
	kb := NewStaticKB()
	tr, secs := MockTrack()
	kb.Add("s1", tr, secs)

	got, found, err := kb.TrackForSession(ctx, "s1")
	if err != nil || !found || got.ID != "mock-gp" {
		t.Fatalf("TrackForSession(s1) = %+v found=%v err=%v", got, found, err)
	}
	sectors, err := kb.Sectors(ctx, "mock-gp")
	if err != nil || len(sectors) != 3 {
		t.Fatalf("Sectors(mock-gp) = %d err=%v, want 3", len(sectors), err)
	}

	// Unknown session -> not found; unknown track -> empty (not an error).
	if _, found, _ := kb.TrackForSession(ctx, "nope"); found {
		t.Error("TrackForSession(nope): found=true, want false")
	}
	if s, err := kb.Sectors(ctx, "nope"); err != nil || len(s) != 0 {
		t.Errorf("Sectors(nope) = %d err=%v, want 0/nil", len(s), err)
	}
}

// TestMockTrack sanity-checks the mock sectors are contiguous and span 0..1.
func TestMockTrack(t *testing.T) {
	_, secs := MockTrack()
	if secs[0].Start != 0 || secs[len(secs)-1].End != 1 {
		t.Errorf("mock sectors don't span 0..1: %+v", secs)
	}
	for i := 1; i < len(secs); i++ {
		if secs[i].Start != secs[i-1].End {
			t.Errorf("mock sectors not contiguous at %d: %+v", i, secs)
		}
	}
}
