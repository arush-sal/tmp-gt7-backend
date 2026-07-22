package processing

import (
	"context"
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
		c, err := postgres.Run(ctx, "timescale/timescaledb:latest-pg16",
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
			PosX: 2, PosY: 2, PosZ: 3, Speed: 120, Throttle: 80, Brake: 0, RPM: 6500, Gear: 3, Yaw: 0.11, AngVelY: 0.22},
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

	want := map[string][2]int{ // {laps, valid}
		"sess-1": {4, 2},
		"sess-3": {4, 2},
		"sess-5": {2, 0},
		"sess-6": {3, 1},
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
