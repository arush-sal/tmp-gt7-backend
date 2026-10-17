package app

import (
	"errors"
	"testing"
	"time"

	telemetryv1 "github.com/arush-sal/garage/backend/gen/gt7/telemetry/v1"
	"github.com/arush-sal/garage/backend/rtps/live"
	"github.com/arush-sal/garage/backend/rtps/processing"
	"github.com/arush-sal/garage/backend/rtps/session"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// stint synthesizes one on-track stint (laps 0..3, in_race clear as in newer GT7 builds) whose content
// depends only on the frame index, so two calls differ only in recv time and package id — exactly
// like an in-game replay of a stored session, or the same capture ingested twice.
func stint(t0 time.Time, pkg0 uint32) []*telemetryv1.TelemetryFrame {
	var out []*telemetryv1.TelemetryFrame
	for i := range 400 {
		lap := int32(i / 100)
		f := &telemetryv1.TelemetryFrame{
			RecvTimestamp: timestamppb.New(t0.Add(time.Duration(i) * 100 * time.Millisecond)),
			PackageId:     pkg0 + uint32(i),
			CarId:         396,
			CurrentLap:    lap,
			TotalLaps:     10,
			CarSpeed:      150,
			PositionX:     float32(i),
			TimeOnTrack:   durationpb.New(time.Duration(i) * 100 * time.Millisecond),
			Source:        telemetryv1.Source_SOURCE_LIVE,
		}
		if lap > 0 && i%100 == 0 {
			f.LastLap = durationpb.New(time.Duration(90_000+i) * time.Millisecond)
		}
		out = append(out, f)
	}
	return append(out, &telemetryv1.TelemetryFrame{ // menu: closes the stint after the debounce
		RecvTimestamp: timestamppb.New(t0.Add(time.Hour)), PackageId: pkg0 + 1000, CarId: 396, CurrentLap: -1, TotalLaps: -1,
	})
}

// TestWithDedup_SecondRunDropped: ingesting the same stint twice into one set of stores keeps only the
// first session; the second leaves no session row, laps or trace samples, the live view points at the
// original (session_id = duplicate_of = original), and the drop is logged once at INFO.
func TestWithDedup_SecondRunDropped(t *testing.T) {
	sessStore := session.NewMemStore()
	lapStore := processing.NewMemLapStore()
	memTrace := processing.NewMemTraceStore()
	traceStore := live.NewAsyncTraceStore(memTrace, 64, nil)
	enrich := live.NewEnrichmentState()
	core, logs := observer.New(zap.InfoLevel)

	var cfg session.Config
	withDedup(&cfg, lapStore, traceStore, enrich, zap.New(core))
	sink := session.NewMultiSink(processing.NewLapSegmenter(lapStore), processing.NewTraceRecorder(traceStore),
		enrichSessionID{enrich})

	t0 := time.Unix(1_800_000_000, 0)
	for run, pkg0 := range []uint32{1000, 50_000} { // a fresh Detector per run, like a fresh stream
		d := session.NewDetector(sink, sessStore, cfg)
		for _, f := range stint(t0.Add(time.Duration(run)*2*time.Hour), pkg0) {
			d.Observe(f)
		}
		d.Close()
	}
	traceStore.Close()

	recs := sessStore.List()
	if len(recs) != 1 {
		t.Fatalf("stored sessions = %d, want 1", len(recs))
	}
	orig := recs[0].ID
	for _, l := range lapStore.List() {
		if l.SessionID != orig {
			t.Errorf("lap %d of dropped session %s persisted", l.LapNumber, l.SessionID)
		}
	}
	if n := len(lapStore.BySession(orig)); n != 4 {
		t.Errorf("original has %d laps, want 4", n)
	}
	for lap := int32(0); lap <= 3; lap++ {
		samples, _ := memTrace.ByLap(orig, lap)
		if len(samples) == 0 {
			t.Errorf("original lap %d has no trace", lap)
		}
	}
	if en := enrich.Get(); en.SessionID != orig || en.DuplicateOf != orig {
		t.Errorf("enrichment session_id=%q duplicate_of=%q, want both %q", en.SessionID, en.DuplicateOf, orig)
	}
	if got := logs.FilterMessage("duplicate session dropped").All(); len(got) != 1 ||
		got[0].ContextMap()["duplicate_of"] != orig {
		t.Errorf("duplicate log entries = %+v, want one with duplicate_of=%s", got, orig)
	}
	if n := logs.FilterMessage("dropped duplicate session has a lap not in the original; still dropping").Len(); n != 0 {
		t.Errorf("divergence warned %d times, want 0", n)
	}
	// The dropped session (id from the log) left no trace samples behind, even those queued async.
	if got := logs.FilterMessage("duplicate session dropped").All(); len(got) == 1 {
		dup, _ := got[0].ContextMap()["session"].(string)
		for lap := int32(0); lap <= 3; lap++ {
			if s, _ := memTrace.ByLap(dup, lap); len(s) != 0 {
				t.Errorf("dropped session %s lap %d has %d trace samples, want 0", dup, lap, len(s))
			}
		}
	}
}

// enrichSessionID mimics EnrichSink's OnSessionStart reset of the live enrichment.
type enrichSessionID struct{ e *live.EnrichmentState }

func (s enrichSessionID) OnSessionStart(r *session.Record)          { s.e.Set(live.Enrichment{SessionID: r.ID}) }
func (enrichSessionID) OnFrame(string, *telemetryv1.TelemetryFrame) {}
func (enrichSessionID) OnSessionEnd(*session.Record)                {}

// TestWithDedup_NoCompletedLapDropped: a stint that never completes a timed lap (here: the first 150
// frames of stint, laps 0 and 1 only) is discarded: no session, laps or trace remain, the drop is
// logged once at INFO, and the live view gets no duplicate link.
func TestWithDedup_NoCompletedLapDropped(t *testing.T) {
	sessStore := session.NewMemStore()
	lapStore := processing.NewMemLapStore()
	memTrace := processing.NewMemTraceStore()
	traceStore := live.NewAsyncTraceStore(memTrace, 64, nil)
	enrich := live.NewEnrichmentState()
	core, logs := observer.New(zap.InfoLevel)

	var cfg session.Config
	withDedup(&cfg, lapStore, traceStore, enrich, zap.New(core))
	d := session.NewDetector(session.NewMultiSink(processing.NewLapSegmenter(lapStore),
		processing.NewTraceRecorder(traceStore), enrichSessionID{enrich}), sessStore, cfg)
	for _, f := range stint(time.Unix(1_800_000_000, 0), 1)[:150] {
		d.Observe(f)
	}
	d.Close()
	traceStore.Close()

	if n := len(sessStore.List()); n != 0 {
		t.Errorf("stored sessions = %d, want 0", n)
	}
	if n := len(lapStore.List()); n != 0 {
		t.Errorf("stored laps = %d, want 0", n)
	}
	got := logs.FilterMessage("session dropped: no completed lap").All()
	if len(got) != 1 {
		t.Fatalf("no-lap drop logged %d times, want 1", len(got))
	}
	dropped, _ := got[0].ContextMap()["session"].(string)
	for lap := int32(0); lap <= 1; lap++ {
		if s, _ := memTrace.ByLap(dropped, lap); len(s) != 0 {
			t.Errorf("lap %d has %d trace samples, want 0", lap, len(s))
		}
	}
	if en := enrich.Get(); en.DuplicateOf != "" || en.SessionID != "" {
		t.Errorf("live session_id=%q duplicate_of=%q, want both empty after a no-lap drop", en.SessionID, en.DuplicateOf)
	}
	if logs.FilterMessage("duplicate session dropped").Len() != 0 {
		t.Error("no-lap drop logged as a duplicate")
	}
}

// TestWithDedup_RecordingOffClearsLiveSession: switching recording off discards the session through
// the same path; the live link is cleared (session_id "") and nothing is stored.
func TestWithDedup_RecordingOffClearsLiveSession(t *testing.T) {
	sessStore := session.NewMemStore()
	lapStore := processing.NewMemLapStore()
	traceStore := live.NewAsyncTraceStore(processing.NewMemTraceStore(), 64, nil)
	defer traceStore.Close()
	enrich := live.NewEnrichmentState()

	var cfg session.Config
	withDedup(&cfg, lapStore, traceStore, enrich, zap.NewNop())
	cfg.Recording = session.NewRecordingControl()
	d := session.NewDetector(session.NewMultiSink(processing.NewLapSegmenter(lapStore),
		processing.NewTraceRecorder(traceStore), enrichSessionID{enrich}), sessStore, cfg)
	frames := stint(time.Unix(1_800_000_000, 0), 1)
	for _, f := range frames[:250] { // into lap 2: confirmed, live session_id set
		d.Observe(f)
	}
	if enrich.Get().SessionID == "" {
		t.Fatal("live session_id not set before the toggle")
	}
	res := make(chan bool, 1)
	go func() { _, _, err := cfg.Recording.Set(false, "", 5*time.Second); res <- err == nil }()
	for i := 250; cfg.Recording.Recording(); i++ { // frames keep flowing until the request is applied
		d.Observe(frames[i])
		time.Sleep(time.Millisecond)
	}
	if !<-res {
		t.Fatal("Set(false) rejected")
	}
	if en := enrich.Get(); en.SessionID != "" || en.DuplicateOf != "" {
		t.Errorf("live session_id=%q duplicate_of=%q, want both empty after recording off", en.SessionID, en.DuplicateOf)
	}
	if n := len(sessStore.List()) + len(lapStore.List()); n != 0 {
		t.Errorf("%d sessions+laps stored after recording off, want 0", n)
	}
}

// failingLookup is a lap store whose fingerprint lookup always errors (DB trouble).
type failingLookup struct{ *processing.MemLapStore }

func (failingLookup) SessionByFingerprint(string) (string, bool, error) {
	return "", false, errors.New("db down")
}

// TestWithDedup_LookupErrorKeepsSession (LOW a): a failed fingerprint lookup skips the dedup decision
// (WARN, treated as not-duplicate) and the session and its laps are stored normally.
func TestWithDedup_LookupErrorKeepsSession(t *testing.T) {
	sessStore := session.NewMemStore()
	lapStore := failingLookup{processing.NewMemLapStore()}
	traceStore := live.NewAsyncTraceStore(processing.NewMemTraceStore(), 64, nil)
	defer traceStore.Close()
	core, logs := observer.New(zap.InfoLevel)

	var cfg session.Config
	withDedup(&cfg, lapStore, traceStore, live.NewEnrichmentState(), zap.New(core))
	d := session.NewDetector(processing.NewLapSegmenter(lapStore), sessStore, cfg)
	for _, f := range stint(time.Unix(1_800_000_000, 0), 1) {
		d.Observe(f)
	}
	d.Close()
	if n := len(sessStore.List()); n != 1 {
		t.Fatalf("stored sessions = %d, want 1", n)
	}
	if n := len(lapStore.List()); n != 4 {
		t.Errorf("stored laps = %d, want 4", n)
	}
	if n := logs.FilterMessage("fingerprint lookup failed; dedup skipped").FilterLevelExact(zap.WarnLevel).Len(); n == 0 {
		t.Error("lookup failure not logged at WARN")
	}
}

// TestWithDedup_LateDuplicateWarned (#5): a later lap matching another stored session keeps the
// session, stores the lap without its fingerprint, and warns once.
func TestWithDedup_LateDuplicateWarned(t *testing.T) {
	sessStore := session.NewMemStore()
	lapStore := processing.NewMemLapStore()
	traceStore := live.NewAsyncTraceStore(processing.NewMemTraceStore(), 64, nil)
	defer traceStore.Close()
	core, logs := observer.New(zap.InfoLevel)
	frames := stint(time.Unix(1_800_000_000, 0), 1)
	// Another stored session already owns lap 2's fingerprint (rollover 2 -> 3 at frame 300).
	_ = lapStore.Save(&processing.Lap{SessionID: "other", LapNumber: 9, Fingerprint: session.LapFingerprint(frames[300], 2)})

	var cfg session.Config
	withDedup(&cfg, lapStore, traceStore, live.NewEnrichmentState(), zap.New(core))
	d := session.NewDetector(processing.NewLapSegmenter(lapStore), sessStore, cfg)
	for _, f := range frames {
		d.Observe(f)
	}
	d.Close()
	if n := len(sessStore.List()); n != 1 {
		t.Fatalf("stored sessions = %d, want 1 (kept)", n)
	}
	laps := lapStore.BySession(sessStore.List()[0].ID)
	if len(laps) != 4 || laps[2].LapNumber != 2 || laps[2].Fingerprint != "" || laps[1].Fingerprint == "" {
		t.Errorf("laps = %+v, want 4 with lap 2 stored unfingerprinted", laps)
	}
	if n := logs.FilterMessage("later lap matches another stored session; session kept").FilterLevelExact(zap.WarnLevel).Len(); n != 1 {
		t.Errorf("late-duplicate WARN logged %d times, want 1", n)
	}
}
