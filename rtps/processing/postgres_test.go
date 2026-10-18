package processing

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/arush-sal/garage/backend/internal/db"
	"github.com/arush-sal/garage/backend/rtps/session"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

// Shared Postgres for this package's integration tests: GT7_TEST_DB_DSN if set, else an ephemeral
// testcontainers Postgres, else skip. (Same pattern as the session package.)
var (
	pgOnce sync.Once
	pgDSN  string
	pgErr  error
)

func sharedPostgres(t *testing.T) string {
	t.Helper()
	pgOnce.Do(func() {
		if dsn := os.Getenv("GT7_TEST_DB_DSN"); dsn != "" {
			pgDSN = dsn
			return
		}
		ctx := context.Background()
		c, err := postgres.Run(ctx, "timescale/timescaledb:2.17.2-pg16",
			postgres.WithDatabase("garage"),
			postgres.WithUsername("test"),
			postgres.WithPassword("test"),
			postgres.BasicWaitStrategies(),
		)
		if err != nil {
			pgErr = err
			return
		}
		pgDSN, pgErr = c.ConnectionString(ctx, "sslmode=disable")
	})
	if pgErr != nil {
		t.Skipf("postgres unavailable (set GT7_TEST_DB_DSN or enable Docker): %v", pgErr)
	}
	return pgDSN
}

// freshPool returns a migrated pool with empty sessions+laps tables.
func freshPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	pool, err := db.Connect(ctx, sharedPostgres(t))
	if err != nil {
		t.Fatalf("db.Connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("db.Migrate: %v", err)
	}
	// sessions CASCADE clears laps (FK); lap_trace has no FK, so truncate it explicitly.
	if _, err := pool.Exec(ctx, "TRUNCATE sessions CASCADE"); err != nil {
		t.Fatalf("truncate sessions: %v", err)
	}
	if _, err := pool.Exec(ctx, "TRUNCATE lap_trace"); err != nil {
		t.Fatalf("truncate lap_trace: %v", err)
	}
	return pool
}

// TestPostgresTraceStore_RoundTrip checks COPY batch insert + ordered ByLap read through the
// TimescaleDB hypertable, including float field fidelity.
func TestPostgresTraceStore_RoundTrip(t *testing.T) {
	pool := freshPool(t)
	// Parent session row (lap_trace FKs to sessions).
	if err := session.NewPostgresStore(pool).Save(&session.Record{
		ID: "s1", StartTime: time.Unix(900, 0).UTC(), Status: session.StatusFinished,
	}); err != nil {
		t.Fatalf("save session: %v", err)
	}
	ts := NewPostgresTraceStore(pool)

	samples := []Sample{
		{RecvTime: time.Unix(1000, 0).UTC(), SessionID: "s1", LapNumber: 1, Seq: 2, Distance: 5.5,
			PosX: 2, PosY: 2, PosZ: 3, Speed: 120, Throttle: 80, Brake: 0, RPM: 6500, Gear: 3, Yaw: 0.11, AngVelY: 0.22,
			SurfaceType: "TCGG", Steering: -0.35},
		{RecvTime: time.Unix(999, 0).UTC(), SessionID: "s1", LapNumber: 1, Seq: 1, Distance: 0,
			PosX: 1, PosY: 2, PosZ: 3, Speed: 100, Throttle: 50, Brake: 0, RPM: 6000, Gear: 3, Yaw: 0.1, AngVelY: 0.2},
		{RecvTime: time.Unix(1002, 0).UTC(), SessionID: "s1", LapNumber: 2, Seq: 3, Distance: 0, Gear: 4},
	}
	if err := ts.SaveBatch(samples); err != nil {
		t.Fatalf("SaveBatch: %v", err)
	}

	lap1, err := ts.ByLap("s1", 1)
	if err != nil {
		t.Fatalf("ByLap: %v", err)
	}
	if len(lap1) != 2 {
		t.Fatalf("lap1 = %d samples, want 2", len(lap1))
	}
	if lap1[0].Seq != 1 || lap1[1].Seq != 2 { // ORDER BY seq
		t.Errorf("lap1 seq order = %d,%d, want 1,2", lap1[0].Seq, lap1[1].Seq)
	}
	if lap1[1].Distance != 5.5 || lap1[1].Speed != 120 || lap1[1].Gear != 3 {
		t.Errorf("field fidelity: %+v", lap1[1])
	}
	if lap1[1].SurfaceType != "TCGG" { // round-trips the per-wheel surface code
		t.Errorf("surface_type = %q, want TCGG", lap1[1].SurfaceType)
	}
	if lap1[1].Steering != -0.35 { // REAL round-trip
		t.Errorf("steering = %v, want -0.35", lap1[1].Steering)
	}
	if lap1[0].Steering != 0 { // unset -> DEFAULT 0
		t.Errorf("steering default = %v, want 0", lap1[0].Steering)
	}
	if lap1[0].SurfaceType != "" { // unset -> DEFAULT '' (not NULL)
		t.Errorf("surface_type default = %q, want empty", lap1[0].SurfaceType)
	}
	lap2, _ := ts.ByLap("s1", 2)
	if len(lap2) != 1 {
		t.Errorf("lap2 = %d samples, want 1", len(lap2))
	}
}

// TestPostgresLapStore_RoundTrip checks field fidelity through the DB. A parent session row must exist
// first (laps FK to sessions).
func TestPostgresLapStore_RoundTrip(t *testing.T) {
	pool := freshPool(t)

	// Parent session (FK target).
	if err := session.NewPostgresStore(pool).Save(&session.Record{
		ID: "s1", StartTime: time.Unix(1000, 0).UTC(), Status: session.StatusFinished,
	}); err != nil {
		t.Fatalf("save session: %v", err)
	}

	ls := NewPostgresLapStore(pool)
	lap := &Lap{
		SessionID:  "s1",
		LapNumber:  1,
		StartTime:  time.Unix(1000, 0).UTC(),
		EndTime:    time.Unix(1090, 0).UTC(),
		Duration:   90500 * time.Millisecond,
		FrameCount: 5400,
		GapCount:   2,
		Complete:   true,
		Timed:      true,
		Valid:      true,
	}
	if err := ls.Save(lap); err != nil {
		t.Fatalf("save lap: %v", err)
	}

	got := ls.BySession("s1")
	if len(got) != 1 {
		t.Fatalf("BySession = %d laps, want 1", len(got))
	}
	g := got[0]
	if !g.StartTime.Equal(lap.StartTime) || !g.EndTime.Equal(lap.EndTime) {
		t.Errorf("times: got %v/%v want %v/%v", g.StartTime, g.EndTime, lap.StartTime, lap.EndTime)
	}
	a, b := *g, *lap
	a.StartTime, a.EndTime, b.StartTime, b.EndTime = time.Time{}, time.Time{}, time.Time{}, time.Time{}
	if a != b {
		t.Errorf("round-trip mismatch:\n got=%+v\nwant=%+v", a, b)
	}
}

// TestPostgresLapStore_Fingerprint covers migration 00008: the fingerprint round-trips, NULL (empty)
// fingerprints never conflict, a repeated fingerprint violates the unique index, SessionByFingerprint
// finds the owner, and DeleteSession removes a session's laps and trace rows.
func TestPostgresLapStore_Fingerprint(t *testing.T) {
	pool := freshPool(t)
	ss := session.NewPostgresStore(pool)
	for _, id := range []string{"s1", "s2"} {
		if err := ss.Save(&session.Record{ID: id, StartTime: time.Unix(1000, 0).UTC(), Status: session.StatusFinished}); err != nil {
			t.Fatalf("save session: %v", err)
		}
	}
	ls := NewPostgresLapStore(pool)
	at := time.Unix(1000, 0).UTC()
	mk := func(sid string, n int32, fp string) *Lap {
		return &Lap{SessionID: sid, LapNumber: n, StartTime: at, EndTime: at, Fingerprint: fp}
	}
	for _, l := range []*Lap{mk("s1", 0, ""), mk("s2", 0, ""), mk("s1", 1, "fp1")} {
		if err := ls.Save(l); err != nil {
			t.Fatalf("save %s#%d: %v", l.SessionID, l.LapNumber, err)
		}
	}
	if err := ls.Save(mk("s2", 1, "fp1")); !errors.Is(err, ErrDuplicateFingerprint) {
		t.Errorf("duplicate fingerprint save = %v, want ErrDuplicateFingerprint", err)
	}
	// A re-save of a stored lap without a fingerprint (the upsert path) never erases the one it has:
	// the original lap must keep its dedup key, or a re-ingest of the same capture is stored again.
	if err := ls.Save(mk("s1", 1, "")); err != nil {
		t.Fatalf("re-save s1#1 without fingerprint: %v", err)
	}
	got := ls.BySession("s1")
	if len(got) != 2 || got[0].Fingerprint != "" || got[1].Fingerprint != "fp1" {
		t.Fatalf("BySession(s1) = %+v, want fingerprints \"\", fp1", got)
	}
	if id, ok, err := ls.SessionByFingerprint("fp1"); err != nil || !ok || id != "s1" {
		t.Errorf("SessionByFingerprint = %q,%v want s1,true", id, ok)
	}
	if _, ok, err := ls.SessionByFingerprint("nope"); ok || err != nil {
		t.Error("unknown fingerprint matched")
	}

	ts := NewPostgresTraceStore(pool)
	if err := ts.SaveBatch([]Sample{{RecvTime: at, SessionID: "s1", LapNumber: 1}}); err != nil {
		t.Fatalf("save trace: %v", err)
	}
	if err := ts.DeleteSession("s1"); err != nil {
		t.Fatalf("trace DeleteSession: %v", err)
	}
	if err := ls.DeleteSession("s1"); err != nil {
		t.Fatalf("lap DeleteSession: %v", err)
	}
	if got := ls.BySession("s1"); len(got) != 0 {
		t.Errorf("laps remain after delete: %+v", got)
	}
	if got, _ := ts.ByLap("s1", 1); len(got) != 0 {
		t.Errorf("trace rows remain after delete: %d", len(got))
	}
	if _, ok, _ := ls.SessionByFingerprint("fp1"); ok {
		t.Error("fingerprint still owned after delete")
	}

	// A query error is reported, never disguised as "not found" (LOW a).
	pool.Close()
	if _, _, err := ls.SessionByFingerprint("fp1"); err == nil {
		t.Error("SessionByFingerprint on a closed pool = nil error, want the query error")
	}
}

// TestPostgresLapStore_Golden runs the real capture through Detector -> LapSegmenter -> Postgres (both
// stores on one pool, so the laps FK to persisted sessions) and asserts the persisted lap breakdown.
func TestPostgresLapStore_Golden(t *testing.T) {
	pool := freshPool(t)
	sessStore := session.NewPostgresStore(pool)
	lapStore := NewPostgresLapStore(pool)

	seg := NewLapSegmenter(lapStore)
	d := session.NewDetector(seg, sessStore, session.Config{NewID: goldenNewID()})
	for _, f := range loadGoldenFrames(t) {
		d.Observe(f)
	}
	d.Close()

	want := map[string][2]int{ // {laps, valid}; sess-3 (race 3, no completed lap) is discarded
		"sess-1": {4, 2},
		"sess-2": {4, 2},
		"sess-4": {3, 1},
	}
	if n := len(sessStore.List()); n != len(want) {
		t.Errorf("persisted %d sessions, want %d", n, len(want))
	}
	for _, s := range sessStore.List() {
		laps := lapStore.BySession(s.ID)
		var valid int
		for _, l := range laps {
			if l.Valid {
				valid++
			}
		}
		w, ok := want[s.ID]
		if !ok {
			t.Errorf("unexpected session %s", s.ID)
			continue
		}
		if len(laps) != w[0] || valid != w[1] {
			t.Errorf("%s: %d laps / %d valid, want %d / %d", s.ID, len(laps), valid, w[0], w[1])
		}
	}
}

// TestPostgresLapStore_TrackReference runs the per-lap track / pin / fastest-lap contract through
// migration 00009, dropping a session the production way (its sessions row, cascading to laps and pin).
func TestPostgresLapStore_TrackReference(t *testing.T) {
	pool := freshPool(t)
	sessions := session.NewPostgresStore(pool)
	for id, total := range map[string]int32{"s1": 0, "s2": 0, "s3": 0, "s4": 0} {
		if err := sessions.Save(&session.Record{ID: id, StartTime: time.Unix(1000, 0).UTC(), Status: session.StatusFinished, TotalLaps: total}); err != nil {
			t.Fatalf("save session: %v", err)
		}
	}
	testTrackRefContract(t, NewPostgresLapStore(pool), func(id string) {
		if err := sessions.Delete(id); err != nil {
			t.Fatalf("delete session: %v", err)
		}
	}, func(id string, n int32) {
		if _, err := pool.Exec(context.Background(), "UPDATE sessions SET total_laps=$2 WHERE id=$1", id, n); err != nil {
			t.Fatalf("set total_laps: %v", err)
		}
	})
}

// TestPostgresLapStore_ClearProvisionalLabels runs the clear-provisional contract against Postgres.
func TestPostgresLapStore_ClearProvisionalLabels(t *testing.T) {
	pool := freshPool(t)
	sessions := session.NewPostgresStore(pool)
	for _, id := range []string{"c1", "c2", "c3", "c4"} {
		if err := sessions.Save(&session.Record{ID: id, StartTime: time.Unix(1000, 0).UTC(), Status: session.StatusFinished}); err != nil {
			t.Fatalf("save session: %v", err)
		}
	}
	testClearProvisionalContract(t, NewPostgresLapStore(pool))
}

// TestPostgresLapStore_LabelTrackSpan runs the bounded-backfill contract against Postgres.
func TestPostgresLapStore_LabelTrackSpan(t *testing.T) {
	pool := freshPool(t)
	sessions := session.NewPostgresStore(pool)
	if err := sessions.Save(&session.Record{ID: "c4", StartTime: time.Unix(1000, 0).UTC(), Status: session.StatusFinished}); err != nil {
		t.Fatalf("save session: %v", err)
	}
	testLabelSpanContract(t, NewPostgresLapStore(pool))
}

// TestPostgresLapStore_SetSessionTrack runs the user-pick relabel contract against Postgres.
func TestPostgresLapStore_SetSessionTrack(t *testing.T) {
	pool := freshPool(t)
	sessions := session.NewPostgresStore(pool)
	for _, id := range []string{"u1", "u2"} {
		if err := sessions.Save(&session.Record{ID: id, StartTime: time.Unix(1000, 0).UTC(), Status: session.StatusFinished}); err != nil {
			t.Fatalf("save session: %v", err)
		}
	}
	testSetSessionTrackContract(t, NewPostgresLapStore(pool))
}
