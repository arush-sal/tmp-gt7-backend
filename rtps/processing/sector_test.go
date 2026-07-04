package processing

import (
	"context"
	"testing"
	"time"

	"github.com/arush-sal/garage/backend/rtps/reference"
)

// tSample builds a trace Sample at a distance + elapsed second + speed (the fields sector metrics use).
func tSample(dist float64, sec int, speed float32) Sample {
	return Sample{Distance: dist, RecvTime: segBase.Add(time.Duration(sec) * time.Second), Speed: speed}
}

// lapFixture: 5 samples over 4s, distances 0..100 (fractions 0,.25,.5,.75,1), speeds 100,80,120,60,140.
func lapFixture() NormalizedLap {
	return Normalize([]Sample{
		tSample(0, 0, 100), tSample(25, 1, 80), tSample(50, 2, 120), tSample(75, 3, 60), tSample(100, 4, 140),
	})
}

// TestMiniSectors: equal-distance, contiguous, spanning 0..1.
func TestMiniSectors(t *testing.T) {
	secs := MiniSectors(4)
	if len(secs) != 4 || secs[0].Start != 0 || secs[3].End != 1 {
		t.Fatalf("MiniSectors(4) = %+v", secs)
	}
	for i := 1; i < len(secs); i++ {
		if secs[i].Start != secs[i-1].End {
			t.Errorf("not contiguous at %d: %+v", i, secs)
		}
	}
	if got := MiniSectors(0); len(got) != 1 { // <=0 -> whole lap
		t.Errorf("MiniSectors(0) = %d sectors, want 1", len(got))
	}
}

// TestSplitSectors: duration from interpolated boundary times; MinSpeed = slowest sample in range.
func TestSplitSectors(t *testing.T) {
	res := SplitSectors(lapFixture(), []reference.Sector{{Index: 0, Start: 0, End: 0.5}, {Index: 1, Start: 0.5, End: 1}})
	if len(res) != 2 {
		t.Fatalf("got %d sectors, want 2", len(res))
	}
	if res[0].Duration != 2*time.Second || res[1].Duration != 2*time.Second {
		t.Errorf("durations = %v, %v, want 2s each", res[0].Duration, res[1].Duration)
	}
	// [0,0.5] covers speeds 100,80,120 -> min 80; [0.5,1] covers 120,60,140 -> min 60.
	if res[0].MinSpeed != 80 || res[1].MinSpeed != 60 {
		t.Errorf("min speeds = %v, %v, want 80, 60", res[0].MinSpeed, res[1].MinSpeed)
	}
}

// TestSectorsForLap_RKBvsFallback: a known session uses the RKB's sectors; an unknown one falls back
// to equal mini-sectors.
func TestSectorsForLap_RKBvsFallback(t *testing.T) {
	ctx := context.Background()
	kb := reference.NewStaticKB()
	tr, secs := reference.MockTrack()
	kb.Add("s1", tr, secs)
	n := lapFixture()

	res, usedRKB, err := SectorsForLap(ctx, kb, "s1", n, 5)
	if err != nil || !usedRKB || len(res) != 3 { // mock track has 3 sectors
		t.Errorf("known session: res=%d usedRKB=%v err=%v, want 3/true/nil", len(res), usedRKB, err)
	}

	res2, usedRKB2, err := SectorsForLap(ctx, kb, "unknown", n, 5)
	if err != nil || usedRKB2 || len(res2) != 5 { // fallback to 5 mini-sectors
		t.Errorf("unknown session: res=%d usedRKB=%v err=%v, want 5/false/nil", len(res2), usedRKB2, err)
	}
}

// TestSplitSectors_EmptyLap: an empty lap yields zero-value results without panicking.
func TestSplitSectors_EmptyLap(t *testing.T) {
	res := SplitSectors(NormalizedLap{}, MiniSectors(3))
	if len(res) != 3 {
		t.Fatalf("got %d sectors, want 3", len(res))
	}
	for _, r := range res {
		if r.Duration != 0 {
			t.Errorf("empty-lap sector %d Duration = %v, want 0", r.Index, r.Duration)
		}
	}
}
