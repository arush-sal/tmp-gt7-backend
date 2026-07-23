package processing_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/arush-sal/garage/backend/collector/protocol"
	telemetryv1 "github.com/arush-sal/garage/backend/gen/gt7/telemetry/v1"
	"github.com/arush-sal/garage/backend/rtps/processing"
	"github.com/arush-sal/garage/backend/rtps/session"
	"github.com/arush-sal/garage/backend/telemetry"
)

// lapFromCapture decodes one VictoryDash telemetry.json (a single real driven lap of already-decrypted
// format-C packets) into a NormalizedLap by driving the PRODUCTION TraceRecorder — so the sample
// distances (cumulative position-delta hypot) and lap segmentation are identical to a live run, not a
// re-implementation. It returns the densest lap's normalized trace (a time-trial capture may include a
// short out/in lap; the flying lap has the most samples). vdTelemetry is declared in
// trackdetect_validation_test.go (same package).
func lapFromCapture(t *testing.T, path string) processing.NormalizedLap {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var tel vdTelemetry
	if err := json.Unmarshal(raw, &tel); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}

	store := processing.NewMemTraceStore()
	rec := &session.Record{ID: "vd-peaks"}
	tr := processing.NewTraceRecorder(store)
	tr.OnSessionStart(rec)

	base := time.Unix(1_700_000_000, 0).UTC()
	var seq uint64
	var lastPID uint32
	var seen bool
	lapCount := map[int32]int{}
	for _, p := range tel.RawUDPPackets {
		if len(p) != 368 {
			continue // not a format-C packet
		}
		pkt := make([]byte, len(p))
		for i, v := range p {
			pkt[i] = byte(v)
		}
		d, err := protocol.Decode(pkt)
		if err != nil {
			continue
		}
		if seen && d.PackageID == lastPID {
			continue // dedup repeats, mirroring the collector
		}
		lastPID, seen = d.PackageID, true

		m := telemetry.CaptureMeta{
			RecvTime: base.Add(time.Duration(seq) * 16 * time.Millisecond), // ~60Hz cadence
			Sequence: seq,
			Source:   telemetryv1.Source_SOURCE_REPLAY,
		}
		tr.OnFrame(rec.ID, telemetry.ToCanonical(d, m))
		lapCount[int32(d.CurrentLap)]++
		seq++
	}
	tr.OnSessionEnd(rec)

	// Pick the densest lap — the flying lap, not a partial out/in lap.
	bestLap, bestN := int32(0), -1
	for lap, cnt := range lapCount {
		if cnt > bestN {
			bestLap, bestN = lap, cnt
		}
	}
	samples, err := store.ByLap(rec.ID, bestLap)
	if err != nil {
		t.Fatalf("ByLap: %v", err)
	}
	if len(samples) < 3 {
		t.Fatalf("densest lap %d has %d samples, want >=3", bestLap, len(samples))
	}
	return processing.Normalize(samples)
}

// TestSpeedExtrema_RealLap is the automated real-data regression gate: on a dense real Suzuka lap, the
// extractor must produce a strictly-alternating peak/valley sequence, and every DetectCorners apex must
// appear as a SpeedExtrema valley (the two speed detectors must not drift apart). Skipped cleanly when
// the external VictoryDash captures are absent, so CI without the data artifacts stays green. The
// end-to-end sign-correctness of delta_ns against a live reference is validated by the replay finisher,
// not committed here.
func TestSpeedExtrema_RealLap(t *testing.T) {
	// week-29-daily-c-ryun is the Suzuka case (supporting-data/victorydash/MANIFEST.md).
	path := filepath.Join("..", "..", "..", "supporting-data", "victorydash", "week-29-daily-c-ryun", "telemetry.json")
	if _, err := os.Stat(path); err != nil {
		t.Skip("VictoryDash captures not present; skipping real-lap peaks validation")
	}

	lap := lapFromCapture(t, path)
	ex := processing.SpeedExtrema(lap, processing.DefaultExtremaConfig())
	if len(ex.Points) == 0 {
		t.Fatalf("no extrema on a full real lap")
	}

	// Strict alternation.
	for i := 1; i < len(ex.Points); i++ {
		if ex.Points[i].Kind == ex.Points[i-1].Kind {
			t.Errorf("points %d,%d both %s — not strictly alternating", i-1, i, ex.Points[i].Kind)
		}
	}

	// Corner consistency: every apex is a speed valley of ours (within 1% of track distance).
	corners := processing.DetectCorners(lap, processing.DefaultCornerConfig())
	if len(corners) == 0 {
		t.Fatalf("DetectCorners found no corners on a full real lap")
	}
	const tol = 0.01
	for _, c := range corners {
		found := false
		for _, p := range ex.Points {
			if p.Kind == processing.Valley && abs(p.Fraction-c.ApexFraction) <= tol {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("corner apex @ fraction %.4f has no matching SpeedExtrema valley", c.ApexFraction)
		}
	}
	t.Logf("real Suzuka lap: %d extrema, %d corners", len(ex.Points), len(corners))
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
