package session

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	telemetryv1 "github.com/arush-sal/garage/backend/gen/gt7/telemetry/v1"
	"github.com/arush-sal/garage/backend/internal/db"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

// Shared Postgres DSN for the package's integration tests, resolved once. Preference: an externally
// supplied GT7_TEST_DB_DSN; otherwise an ephemeral testcontainers Postgres. If neither is available
// (no env, no usable Docker), the pg tests skip — so `go test ./...` still passes without Docker.
// The container is reaped by testcontainers' Ryuk on process exit, so no manual teardown is needed.
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

// freshPGStore returns a PostgresStore over a migrated pool with an empty sessions table (truncated
// so each test asserts against a known-empty starting point despite the shared database).
func freshPGStore(t *testing.T) *PostgresStore {
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
	if _, err := pool.Exec(ctx, "TRUNCATE sessions CASCADE"); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return NewPostgresStore(pool)
}

// TestPostgresStore_RoundTrip checks full field fidelity through the DB, including the enum Source,
// nanosecond durations, and a NULL end_time for an active (not-yet-closed) session.
func TestPostgresStore_RoundTrip(t *testing.T) {
	st := freshPGStore(t)

	finished := &Record{
		ID:             "s-fin",
		StartTime:      time.Unix(1000, 0).UTC(),
		EndTime:        time.Unix(1100, 0).UTC(),
		CarID:          2108,
		CarName:        "Dodge SRT Tomahawk X Vision Gran Turismo",
		Source:         telemetryv1.Source_SOURCE_LIVE,
		FrameCount:     1200,
		GapCount:       3,
		FirstPackageID: 500,
		LastPackageID:  1700,
		CurrentLap:     3,
		TotalLaps:      2,
		BestLap:        90500 * time.Millisecond,
		LastLap:        91200 * time.Millisecond,
		Status:         StatusFinished,
	}
	if err := st.Save(finished); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, ok := st.Get("s-fin")
	if !ok {
		t.Fatal("Get(s-fin): not found")
	}
	// Compare times with Equal (pgx returns timestamptz in a different *Location, so == would fail on
	// the same instant), then compare the remaining fields with the times zeroed out.
	if !got.StartTime.Equal(finished.StartTime) || !got.EndTime.Equal(finished.EndTime) {
		t.Errorf("time mismatch: start %v/%v end %v/%v",
			got.StartTime, finished.StartTime, got.EndTime, finished.EndTime)
	}
	a, b := *got, *finished
	a.StartTime, a.EndTime, b.StartTime, b.EndTime = time.Time{}, time.Time{}, time.Time{}, time.Time{}
	if a != b {
		t.Errorf("round-trip mismatch (non-time fields):\n got=%+v\nwant=%+v", a, b)
	}

	// Active session: zero EndTime must persist as NULL and come back zero.
	active := &Record{ID: "s-act", StartTime: time.Unix(2000, 0).UTC(), CarID: 1384, Status: StatusActive}
	if err := st.Save(active); err != nil {
		t.Fatalf("Save active: %v", err)
	}
	gotA, _ := st.Get("s-act")
	if !gotA.EndTime.IsZero() {
		t.Errorf("active EndTime = %v, want zero (NULL)", gotA.EndTime)
	}
}

// TestPostgresStore_Upsert checks that re-saving an id updates in place (active -> finished) without
// creating a duplicate row.
func TestPostgresStore_Upsert(t *testing.T) {
	st := freshPGStore(t)
	rec := &Record{ID: "s1", StartTime: time.Unix(1, 0).UTC(), CarID: 1, Status: StatusActive}
	_ = st.Save(rec)
	rec.EndTime = time.Unix(2, 0).UTC()
	rec.Status = StatusFinished
	_ = st.Save(rec)

	got, _ := st.Get("s1")
	if got.Status != StatusFinished {
		t.Errorf("status = %q, want finished", got.Status)
	}
	if n := len(st.List()); n != 1 {
		t.Errorf("List len = %d, want 1 (no duplicate)", n)
	}
}

// TestPostgresStore_ListOrder checks List returns sessions ordered by start time.
func TestPostgresStore_ListOrder(t *testing.T) {
	st := freshPGStore(t)
	_ = st.Save(&Record{ID: "b", StartTime: time.Unix(200, 0).UTC(), Status: StatusFinished})
	_ = st.Save(&Record{ID: "a", StartTime: time.Unix(100, 0).UTC(), Status: StatusFinished})
	_ = st.Save(&Record{ID: "c", StartTime: time.Unix(300, 0).UTC(), Status: StatusFinished})

	got := st.List()
	want := []string{"a", "b", "c"} // by start_time
	if len(got) != 3 {
		t.Fatalf("List len = %d, want 3", len(got))
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Errorf("List[%d].ID = %q, want %q", i, got[i].ID, id)
		}
	}
}

// TestPostgresStore_DetectorGolden runs the real 4-race capture through the Detector wired to
// Postgres — the full path SM will use in production — and asserts the persisted result: 4 sessions,
// race 3 abandoned.
func TestPostgresStore_DetectorGolden(t *testing.T) {
	st := freshPGStore(t)
	frames := loadGolden(t)

	d := NewDetector(NoopSink{}, st, Config{})
	for _, f := range frames {
		d.Observe(f)
	}
	d.Close()

	got := st.List()
	if len(got) != 4 {
		t.Fatalf("persisted %d sessions, want 4", len(got))
	}
	var abandoned int
	for _, s := range got {
		if s.Status == StatusAbandoned {
			abandoned++
			if s.TotalLaps != 10 {
				t.Errorf("abandoned TotalLaps = %d, want 10 (race 3)", s.TotalLaps)
			}
		}
	}
	if abandoned != 1 {
		t.Errorf("abandoned = %d, want 1", abandoned)
	}
}
