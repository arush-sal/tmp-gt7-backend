package analysis

import (
	"testing"
	"time"

	"github.com/arush-sal/garage/backend/rtps/processing"
)

const s = time.Second

// TestPersonalBest picks the fastest VALID lap, ignoring faster-but-invalid laps.
func TestPersonalBest(t *testing.T) {
	laps := []processing.Lap{
		{LapNumber: 0, Duration: 30 * s, Valid: false}, // formation: faster but invalid
		{LapNumber: 1, Duration: 92 * s, Valid: true},
		{LapNumber: 2, Duration: 90 * s, Valid: true},  // fastest valid
		{LapNumber: 3, Duration: 88 * s, Valid: false}, // gapped: faster but invalid
	}
	pb, ok := PersonalBest(laps)
	if !ok || pb.LapNumber != 2 {
		t.Errorf("PB = lap %d ok=%v, want lap 2", pb.LapNumber, ok)
	}
	if _, ok := PersonalBest([]processing.Lap{{Valid: false}}); ok {
		t.Error("no valid lap: ok=true, want false")
	}
}

// TestConsistency: best/spread/mean over valid laps only.
func TestConsistency(t *testing.T) {
	c := consistencyOf([]processing.Lap{
		{Duration: 90 * s, Valid: true},
		{Duration: 94 * s, Valid: true},
		{Duration: 92 * s, Valid: true},
		{Duration: 10 * s, Valid: false}, // excluded
	})
	if c.Count != 3 || c.Best != 90*s || c.Spread != 4*s || c.Mean != 92*s {
		t.Errorf("consistency = %+v, want count3 best90 spread4 mean92", c)
	}
}

// TestConsistency_SingleLap: one valid lap -> zero spread/std-dev, best==mean==that lap.
func TestConsistency_SingleLap(t *testing.T) {
	c := consistencyOf([]processing.Lap{{Duration: 90 * s, Valid: true}})
	if c.Count != 1 || c.Best != 90*s || c.Mean != 90*s || c.Spread != 0 || c.StdDev != 0 {
		t.Errorf("single-lap consistency = %+v, want count1 best/mean90 spread0 stddev0", c)
	}
	if c := consistencyOf(nil); c.Count != 0 {
		t.Errorf("empty consistency count = %d, want 0", c.Count)
	}
}

// TestTheoreticalBest: per-sector fastest across laps -> optimal time; each best notes its source lap.
func TestTheoreticalBest(t *testing.T) {
	perLap := map[int32][]processing.SectorResult{
		1: {{Index: 0, Duration: 30 * s}, {Index: 1, Duration: 32 * s}}, // lap1: S1 best
		2: {{Index: 0, Duration: 29 * s}, {Index: 1, Duration: 34 * s}}, // lap2: S0 best
	}
	total, best := theoreticalBest(perLap)
	if total != 61*s { // 29 (S0, lap2) + 32 (S1, lap1)
		t.Errorf("theoretical best = %v, want 61s", total)
	}
	if len(best) != 2 || best[0].LapNumber != 2 || best[1].LapNumber != 1 {
		t.Errorf("best sectors = %+v, want S0<-lap2, S1<-lap1", best)
	}
	// A lap sums to 62s (30+32) and 63s (29+34); theoretical best 61s beats both.
	if total >= 62*s {
		t.Errorf("theoretical best %v should beat every actual lap", total)
	}
}

// TestSectorDeltas: aligns by Index; sectors present in only one side are skipped.
func TestSectorDeltas(t *testing.T) {
	lap := []processing.SectorResult{{Index: 0, Duration: 38 * s}, {Index: 1, Duration: 32 * s}}
	pb := []processing.SectorResult{{Index: 0, Duration: 36 * s}, {Index: 1, Duration: 31 * s}, {Index: 2, Duration: 20 * s}}
	d := sectorDeltas(lap, pb)
	if len(d) != 2 {
		t.Fatalf("got %d deltas, want 2 (unmatched sector skipped)", len(d))
	}
	if d[0].DeltaToPB != 2*s || d[1].DeltaToPB != 1*s {
		t.Errorf("deltas = %v, %v, want +2s, +1s", d[0].DeltaToPB, d[1].DeltaToPB)
	}
}
