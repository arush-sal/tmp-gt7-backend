package processing_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/arush-sal/garage/backend/collector/protocol"
	"github.com/arush-sal/garage/backend/rtps/processing"
	"github.com/arush-sal/garage/backend/rtps/rkb"
	"github.com/arush-sal/garage/backend/rtps/session"
)

// vdCase pairs a VictoryDash capture directory (one real driven GT7 lap) with the RKB slug the track
// detector is expected to resolve it to. Sourced from supporting-data/victorydash/MANIFEST.md.
type vdCase struct {
	dir      string
	wantSlug string
}

// vdCases is the full VictoryDash ground-truth set: 11 real driven laps across 11 distinct measured
// tracks. Each lap must resolve to its own slug (these are VictoryDash-MEASURED tracks with distinct
// geometry, so the forward/reverse twin collapse is not expected here).
var vdCases = []vdCase{
	{"interlagos-time-trial-a-tapai", "interlagos"},
	{"spa-time-trial-antonio-santos", "spa-francorchamps"},
	{"week-27-daily-a-sirvia", "autopolis-shortcut"},
	{"week-27-daily-c-c-birtles", "mount-panorama"},
	{"week-27-daily-c-k-drumont", "deep-forest-2"},
	{"week-28-daily-a-sardegna", "sardegna-road-a"},
	{"week-28-daily-b-spa", "spa-francorchamps"},
	{"week-28-daily-c-watkins-glen", "watkins-glen-long"},
	{"week-29-daily-a-a-ruizz", "willow-springs-big-willow"},
	{"week-29-daily-b-a-haydar", "nurburgring-gp"},
	{"week-29-daily-c-ryun", "suzuka"},
}

// vdTelemetry mirrors the VictoryDash telemetry.json shape: rawUdpPackets are ALREADY-DECRYPTED
// 368-byte format-C GT7 packets (magic 0x47375330 in plaintext), so each packet's bytes go straight to
// protocol.Decode with no Decrypt step (see tools/rkb/vd2csv/main.go for the prior art).
type vdTelemetry struct {
	RawUDPPackets [][]int `json:"rawUdpPackets"`
	LapTimeMs     int     `json:"lapTimeMs"`
}

// drivenPathFromCapture decodes one telemetry.json into the world-metre driven path (position_x,
// position_z), dropping exact repeated PackageIDs. This is a simplified dedup for ordered fixture
// data; the production collector Deduper (collector/protocol/dedup.go) additionally handles backward
// reorders and resets, which these clean single-lap captures do not exercise.
func drivenPathFromCapture(t *testing.T, path string) [][2]float64 {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var tel vdTelemetry
	if err := json.Unmarshal(raw, &tel); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}

	var points [][2]float64
	var lastPID uint32
	var seen bool
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
		points = append(points, [2]float64{float64(d.PositionX), float64(d.PositionZ)})
	}
	return points
}

// TestTrackDetector_RealCaptures is the Task-8 capture-validation + threshold gate: it replays each
// real VictoryDash driven lap through the REAL rkb.SQLiteStore geometry and a capturing enricher, and
// asserts every lap resolves to its expected slug ("confident") with NONE resolving to a wrong slug.
// Skipped cleanly when the ground-truth captures or the generated rkb.db are absent (same pattern as
// the round-trip gate in TestTrackDetector_ForwardReverseTwin_RealCorpus), so CI without the data
// artifacts stays green.
//
// SCOPE (honest): this is a fixture-backed REGRESSION gate, not proof of independent generalization.
// Each track's RKB reference geometry was derived from the SAME VictoryDash capture directory as the
// driven lap tested here (shared provenance), so "11/11" means the matcher + these thresholds
// correctly re-identify VictoryDash-sourced tracks - it is NOT yet evidence of resolving an
// independent live PS5 capture. Follow-up: validate at least one non-VictoryDash capture per track.
func TestTrackDetector_RealCaptures(t *testing.T) {
	vdDir := filepath.Join("..", "..", "..", "supporting-data", "victorydash")
	if _, err := os.Stat(vdDir); err != nil {
		t.Skip("VictoryDash ground-truth captures not present; skipping real-capture validation")
	}
	dbPath := filepath.Join("..", "..", "tools", "rkb", "out", "rkb.db")
	if _, err := os.Stat(dbPath); err != nil {
		t.Skip("generated rkb.db not present; run `cd tools/rkb && ./.venv/bin/python -m rkb`")
	}

	st, err := rkb.Open(dbPath)
	if err != nil {
		t.Fatalf("open rkb.db: %v", err)
	}
	defer func() { _ = st.Close() }()

	geoms, err := st.DetectionGeometry(context.Background())
	if err != nil {
		t.Fatalf("detection geometry: %v", err)
	}
	detectable := processing.FilterDetectableForTest(geoms)

	for _, tc := range vdCases {
		tc := tc
		t.Run(tc.dir, func(t *testing.T) {
			path := filepath.Join(vdDir, tc.dir, "telemetry.json")
			points := drivenPathFromCapture(t, path)
			if len(points) == 0 {
				t.Fatalf("%s: no driven points decoded", tc.dir)
			}

			// Diagnostics: log the correct candidate's rank/score against the best wrong candidate, so
			// tuning has the numbers it needs. Run against the same detectable set the detector uses.
			diags := processing.CandidateDiagnosticsForTest(points, detectable)
			logDiagnostics(t, tc.wantSlug, diags)

			// Drive the lap through the real detector wired to the real store.
			enrich := &fakeEnricher{}
			d := processing.NewTrackDetector(st, enrich, nil)
			rec := &session.Record{ID: "s-" + tc.dir}
			d.OnSessionStart(rec)
			feedFrames(d, rec.ID, points)
			d.OnSessionEnd(rec)

			if enrich.callCount() == 0 {
				t.Errorf("%s: expected resolve to %q, got NO match (stayed nil)", tc.dir, tc.wantSlug)
				return
			}
			got := enrich.lastCall()
			if got.slug != tc.wantSlug {
				t.Errorf("%s: WRONG match: resolved %q, expected %q (confidence %q)",
					tc.dir, got.slug, tc.wantSlug, got.confidence)
				return
			}
			if got.confidence != "confident" {
				t.Errorf("%s: expected confidence %q, got %q", tc.dir, "confident", got.confidence)
			}
		})
	}
}

// logDiagnostics logs the top candidates by score plus the expected candidate's rank/score, so a
// failing tune shows exactly how far the correct track sat from the winner and whether its bbox
// pre-filter passed.
func logDiagnostics(t *testing.T, want string, diags []processing.CandidateDiag) {
	t.Helper()
	top := 5
	if len(diags) < top {
		top = len(diags)
	}
	for i := 0; i < top; i++ {
		d := diags[i]
		t.Logf("  #%d %-32s score=%.2f hasBbox=%v bboxPass=%v", i+1, d.Slug, d.Score, d.HasBbox, d.BboxPass)
	}
	for i, d := range diags {
		if d.Slug == want {
			t.Logf("  EXPECTED %q rank=%d/%d score=%.2f hasBbox=%v bboxPass=%v maxPoke=%.1fm",
				want, i+1, len(diags), d.Score, d.HasBbox, d.BboxPass, d.MaxPoke)
			return
		}
	}
	t.Logf("  EXPECTED %q NOT FOUND in candidate set", want)
}
