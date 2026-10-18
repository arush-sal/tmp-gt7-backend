package db

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

// serverDSN returns a Postgres server to run the migration test on: GT7_TEST_DB_DSN if set, else an
// ephemeral testcontainers Postgres, else the test is skipped. The test makes its own database on it.
func serverDSN(t *testing.T) string {
	t.Helper()
	if dsn := os.Getenv("GT7_TEST_DB_DSN"); dsn != "" {
		return dsn
	}
	ctx := context.Background()
	c, err := postgres.Run(ctx, "timescale/timescaledb:2.17.2-pg16",
		postgres.WithDatabase("garage"), postgres.WithUsername("test"), postgres.WithPassword("test"),
		postgres.BasicWaitStrategies())
	if err != nil {
		t.Skipf("postgres unavailable (set GT7_TEST_DB_DSN or enable Docker): %v", err)
	}
	t.Cleanup(func() { _ = c.Terminate(ctx) })
	dsn, err := c.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	return dsn
}

// TestMigration00010_ClearsProvisionalLabels applies migrations up to 00009 on a database holding
// provisional, confident and unlabelled laps, then 00010: only the provisional labels are cleared.
func TestMigration00010_ClearsProvisionalLabels(t *testing.T) {
	ctx := context.Background()
	admin, err := Connect(ctx, serverDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	dbName := fmt.Sprintf("mig10_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+dbName); err != nil {
		t.Fatal(err)
	}
	cfg := admin.Config().ConnConfig.Copy()
	cfg.Database = dbName
	pool, err := pgxpool.New(ctx, cfg.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	sqlDB := stdlib.OpenDBFromPool(pool)
	defer func() { _ = sqlDB.Close() }()
	goose.SetBaseFS(migrationsFS)
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpToContext(ctx, sqlDB, "migrations", 9); err != nil {
		t.Fatalf("up to 9: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO sessions (id, start_time, car_id, source, frame_count, gap_count, first_package_id, last_package_id,
 current_lap, total_laps, best_lap_ns, last_lap_ns, status) VALUES ('s1', now(), 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 'finished'), ('s2', now(), 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 'active')`); err != nil {
		t.Fatalf("insert sessions: %v", err)
	}
	// Laps 1-3 are in a finished session; lap 4 is a provisional lap of a session a crash left 'active'.
	for i, r := range []struct{ slug, conf *string }{
		{ptr("suzuka"), ptr("confident")}, {ptr("interlagos"), ptr("provisional")}, {nil, nil}, {ptr("barcelona"), ptr("provisional")},
	} {
		sid := "s1"
		if i == 3 {
			sid = "s2"
		}
		if _, err := pool.Exec(ctx, `INSERT INTO laps (session_id, lap_number, start_time, end_time, duration_ns, frame_count, gap_count, complete, timed, valid,
 track_slug, track_confidence) VALUES ($4, $1, now(), now(), 1, 0, 0, true, true, true, $2, $3)`, i+1, r.slug, r.conf, sid); err != nil {
			t.Fatalf("insert lap: %v", err)
		}
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	want := map[int][2]string{1: {"suzuka", "confident"}, 2: {"", ""}, 3: {"", ""}, 4: {"", ""}}
	rows, err := pool.Query(ctx, `SELECT lap_number, COALESCE(track_slug,''), COALESCE(track_confidence,'') FROM laps`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var num int
		var slug, conf string
		if err := rows.Scan(&num, &slug, &conf); err != nil {
			t.Fatal(err)
		}
		n++
		if w := want[num]; slug != w[0] || conf != w[1] {
			t.Errorf("lap %d = %q/%q, want %q/%q", num, slug, conf, w[0], w[1])
		}
	}
	if n != 4 {
		t.Errorf("%d laps, want 4", n)
	}
}

func ptr(s string) *string { return &s }
