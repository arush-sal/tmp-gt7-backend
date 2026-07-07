package session

import (
	"context"
	"errors"
	"fmt"
	"time"

	telemetryv1 "github.com/arush-sal/garage/backend/gen/gt7/telemetry/v1"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore is the persistent Store implementation (D-B1 reversed for SM: sessions persist). It
// backs the same Store interface as MemStore, so the Detector and ingest server are unchanged. It
// borrows a shared pgx pool (created + migrated by internal/db) and does not own its lifecycle.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore returns a store over an already-connected, already-migrated pool.
func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

// upsertSQL inserts a session or updates every mutable column on id conflict (the Detector saves a
// session active, then again on close).
const upsertSQL = `
INSERT INTO sessions
 (id, start_time, end_time, car_id, source, frame_count, gap_count,
  first_package_id, last_package_id, current_lap, total_laps, best_lap_ns, last_lap_ns, status, car_name)
 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
 ON CONFLICT (id) DO UPDATE SET
   start_time=EXCLUDED.start_time, end_time=EXCLUDED.end_time, car_id=EXCLUDED.car_id,
   source=EXCLUDED.source, frame_count=EXCLUDED.frame_count, gap_count=EXCLUDED.gap_count,
   first_package_id=EXCLUDED.first_package_id, last_package_id=EXCLUDED.last_package_id,
   current_lap=EXCLUDED.current_lap, total_laps=EXCLUDED.total_laps,
   best_lap_ns=EXCLUDED.best_lap_ns, last_lap_ns=EXCLUDED.last_lap_ns,
   status=EXCLUDED.status, car_name=EXCLUDED.car_name, updated_at=now()`

// Save upserts the record. Counts (uint64) and package ids (uint32) widen to bigint; durations store
// as nanoseconds; a zero EndTime (active session) is written as SQL NULL. The uint64->int64 cast is
// safe by construction: at 60 Hz a frame/gap count cannot approach int64's range, and package ids are
// uint32. The DB also CHECKs these columns are non-negative (migration) as defense-in-depth.
func (s *PostgresStore) Save(rec *Record) error {
	var endTime *time.Time
	if !rec.EndTime.IsZero() {
		t := rec.EndTime
		endTime = &t
	}
	_, err := s.pool.Exec(context.Background(), upsertSQL,
		rec.ID, rec.StartTime, endTime, rec.CarID, int32(rec.Source),
		int64(rec.FrameCount), int64(rec.GapCount),
		int64(rec.FirstPackageID), int64(rec.LastPackageID),
		rec.CurrentLap, rec.TotalLaps,
		rec.BestLap.Nanoseconds(), rec.LastLap.Nanoseconds(), string(rec.Status), rec.CarName)
	if err != nil {
		return fmt.Errorf("session: save %s: %w", rec.ID, err)
	}
	return nil
}

const selectCols = `id, start_time, end_time, car_id, source, frame_count, gap_count,
 first_package_id, last_package_id, current_lap, total_laps, best_lap_ns, last_lap_ns, status, car_name`

// Get returns the record for id; found is false when absent.
func (s *PostgresStore) Get(id string) (*Record, bool) {
	row := s.pool.QueryRow(context.Background(), "SELECT "+selectCols+" FROM sessions WHERE id=$1", id)
	rec, err := scanRecord(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false
	}
	if err != nil {
		return nil, false
	}
	return rec, true
}

// List returns all sessions ordered by start time (then id) — a stable, meaningful order for the
// future "recent sessions" view.
func (s *PostgresStore) List() []*Record {
	rows, err := s.pool.Query(context.Background(), "SELECT "+selectCols+" FROM sessions ORDER BY start_time, id")
	if err != nil {
		return nil
	}
	defer rows.Close()

	var out []*Record
	for rows.Next() {
		rec, err := scanRecord(rows)
		if err != nil {
			return out
		}
		out = append(out, rec)
	}
	return out
}

// rowScanner is satisfied by both pgx.Row (QueryRow) and pgx.Rows (Query), so scanRecord serves Get
// and List alike.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanRecord maps one DB row back to a Record, narrowing the widened integer columns and converting
// nanoseconds + a nullable end_time back to Go types.
func scanRecord(row rowScanner) (*Record, error) {
	var (
		rec                         Record
		endTime                     *time.Time
		source                      int32
		frameCount, gapCount        int64
		firstPackageID, lastPackage int64
		bestNs, lastNs              int64
		status                      string
	)
	if err := row.Scan(
		&rec.ID, &rec.StartTime, &endTime, &rec.CarID, &source,
		&frameCount, &gapCount, &firstPackageID, &lastPackage,
		&rec.CurrentLap, &rec.TotalLaps, &bestNs, &lastNs, &status, &rec.CarName,
	); err != nil {
		return nil, err
	}
	if endTime != nil {
		rec.EndTime = *endTime
	}
	rec.Source = telemetryv1.Source(source)
	rec.FrameCount = uint64(frameCount)
	rec.GapCount = uint64(gapCount)
	rec.FirstPackageID = uint32(firstPackageID)
	rec.LastPackageID = uint32(lastPackage)
	rec.BestLap = time.Duration(bestNs)
	rec.LastLap = time.Duration(lastNs)
	rec.Status = Status(status)
	return &rec, nil
}
