package processing

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresLapStore is the persistent LapStore (D-B12 next step). It backs the same LapStore interface
// as MemLapStore over a shared pgx pool (created + migrated by internal/db), so the segmenter is
// unchanged.
type PostgresLapStore struct {
	pool *pgxpool.Pool
}

// NewPostgresLapStore returns a lap store over an already-connected, already-migrated pool.
func NewPostgresLapStore(pool *pgxpool.Pool) *PostgresLapStore {
	return &PostgresLapStore{pool: pool}
}

// lapUpsertSQL inserts a lap or replaces it on (session_id, lap_number) conflict — idempotent, though
// the segmenter emits each lap once.
const lapUpsertSQL = `
INSERT INTO laps
 (session_id, lap_number, start_time, end_time, duration_ns, frame_count, gap_count, complete, timed, valid)
 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
 ON CONFLICT (session_id, lap_number) DO UPDATE SET
   start_time=EXCLUDED.start_time, end_time=EXCLUDED.end_time, duration_ns=EXCLUDED.duration_ns,
   frame_count=EXCLUDED.frame_count, gap_count=EXCLUDED.gap_count, complete=EXCLUDED.complete,
   timed=EXCLUDED.timed, valid=EXCLUDED.valid`

// Save upserts a lap. Duration stores as nanoseconds; counts widen to bigint (CHECK (>= 0) in the DB).
func (s *PostgresLapStore) Save(lap *Lap) error {
	_, err := s.pool.Exec(context.Background(), lapUpsertSQL,
		lap.SessionID, lap.LapNumber, lap.StartTime, lap.EndTime, lap.Duration.Nanoseconds(),
		int64(lap.FrameCount), int64(lap.GapCount), lap.Complete, lap.Timed, lap.Valid)
	if err != nil {
		return fmt.Errorf("processing: save lap %s#%d: %w", lap.SessionID, lap.LapNumber, err)
	}
	return nil
}

const lapCols = `session_id, lap_number, start_time, end_time, duration_ns, frame_count, gap_count, complete, timed, valid`

// BySession returns a session's laps ordered by lap number (nil if none).
func (s *PostgresLapStore) BySession(sessionID string) []*Lap {
	rows, err := s.pool.Query(context.Background(),
		"SELECT "+lapCols+" FROM laps WHERE session_id=$1 ORDER BY lap_number", sessionID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	return scanLaps(rows)
}

// List returns all laps ordered by start time then lap number (groups a session's laps together,
// sessions in chronological order).
func (s *PostgresLapStore) List() []*Lap {
	rows, err := s.pool.Query(context.Background(),
		"SELECT "+lapCols+" FROM laps ORDER BY start_time, lap_number")
	if err != nil {
		return nil
	}
	defer rows.Close()
	return scanLaps(rows)
}

// scanLaps maps rows to Laps, narrowing the widened columns and converting nanoseconds back to a
// Duration.
func scanLaps(rows pgx.Rows) []*Lap {
	var out []*Lap
	for rows.Next() {
		var (
			lap           Lap
			durNs, fc, gc int64
		)
		if err := rows.Scan(&lap.SessionID, &lap.LapNumber, &lap.StartTime, &lap.EndTime,
			&durNs, &fc, &gc, &lap.Complete, &lap.Timed, &lap.Valid); err != nil {
			return out
		}
		lap.Duration = time.Duration(durNs)
		lap.FrameCount = uint64(fc)
		lap.GapCount = uint64(gc)
		out = append(out, &lap)
	}
	return out
}
