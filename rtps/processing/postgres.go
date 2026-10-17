package processing

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
// the segmenter emits each lap once. An empty fingerprint is stored as NULL so the unique index on
// laps.fingerprint (migration 00008) only constrains real fingerprints.
const lapUpsertSQL = `
INSERT INTO laps
 (session_id, lap_number, start_time, end_time, duration_ns, frame_count, gap_count, complete, timed, valid, fingerprint)
 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NULLIF($11,''))
 ON CONFLICT (session_id, lap_number) DO UPDATE SET
   start_time=EXCLUDED.start_time, end_time=EXCLUDED.end_time, duration_ns=EXCLUDED.duration_ns,
   frame_count=EXCLUDED.frame_count, gap_count=EXCLUDED.gap_count, complete=EXCLUDED.complete,
   timed=EXCLUDED.timed, valid=EXCLUDED.valid, fingerprint=EXCLUDED.fingerprint`

// Save upserts a lap. Duration stores as nanoseconds; counts widen to bigint (CHECK (>= 0) in the DB).
func (s *PostgresLapStore) Save(lap *Lap) error {
	_, err := s.pool.Exec(context.Background(), lapUpsertSQL,
		lap.SessionID, lap.LapNumber, lap.StartTime, lap.EndTime, lap.Duration.Nanoseconds(),
		int64(lap.FrameCount), int64(lap.GapCount), lap.Complete, lap.Timed, lap.Valid, lap.Fingerprint)
	if pgErr := (*pgconn.PgError)(nil); errors.As(err, &pgErr) && pgErr.ConstraintName == "laps_fingerprint_key" {
		// The single INSERT failed atomically, so nothing was half-written; let the caller retry.
		return fmt.Errorf("processing: save lap %s#%d: %w", lap.SessionID, lap.LapNumber, ErrDuplicateFingerprint)
	}
	if err != nil {
		return fmt.Errorf("processing: save lap %s#%d: %w", lap.SessionID, lap.LapNumber, err)
	}
	return nil
}

const lapCols = `session_id, lap_number, start_time, end_time, duration_ns, frame_count, gap_count, complete, timed, valid, COALESCE(fingerprint, '')`

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

// SessionByFingerprint returns the session owning a lap with fingerprint fp (uses the unique index).
// No row is not-found; any other error is returned so the caller can tell a failed lookup apart.
func (s *PostgresLapStore) SessionByFingerprint(fp string) (string, bool, error) {
	if fp == "" {
		return "", false, nil
	}
	var id string
	err := s.pool.QueryRow(context.Background(), "SELECT session_id FROM laps WHERE fingerprint=$1", fp).Scan(&id)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return "", false, nil
	case err != nil:
		return "", false, fmt.Errorf("processing: fingerprint lookup: %w", err)
	}
	return id, true, nil
}

// DeleteSession removes a session's laps.
func (s *PostgresLapStore) DeleteSession(sessionID string) error {
	if _, err := s.pool.Exec(context.Background(), "DELETE FROM laps WHERE session_id=$1", sessionID); err != nil {
		return fmt.Errorf("processing: delete laps of %s: %w", sessionID, err)
	}
	return nil
}

// PostgresTraceStore bulk-loads trace samples into the lap_trace hypertable via COPY.
type PostgresTraceStore struct {
	pool *pgxpool.Pool
}

// NewPostgresTraceStore returns a trace store over an already-connected, already-migrated pool.
func NewPostgresTraceStore(pool *pgxpool.Pool) *PostgresTraceStore {
	return &PostgresTraceStore{pool: pool}
}

var traceColumns = []string{
	"recv_timestamp", "session_id", "lap_number", "seq", "distance",
	"pos_x", "pos_y", "pos_z", "speed", "throttle", "brake", "rpm", "gear", "yaw", "ang_vel_y",
	"fuel", "tyre_temp_fl", "tyre_temp_fr", "tyre_temp_rl", "tyre_temp_rr", "clutch", "boost", "delta_to_best_ns",
	"surface_type", "steering",
}

// SaveBatch COPY-loads a batch of samples — the efficient bulk path for high-rate trace ingest.
func (s *PostgresTraceStore) SaveBatch(samples []Sample) error {
	if len(samples) == 0 {
		return nil
	}
	rows := make([][]any, len(samples))
	for i, m := range samples {
		rows[i] = []any{
			m.RecvTime, m.SessionID, m.LapNumber, int64(m.Seq), m.Distance,
			m.PosX, m.PosY, m.PosZ, m.Speed, m.Throttle, m.Brake, m.RPM, m.Gear, m.Yaw, m.AngVelY,
			m.Fuel, m.TyreTempFL, m.TyreTempFR, m.TyreTempRL, m.TyreTempRR, m.Clutch, m.Boost,
			m.DeltaToBestNs, // *int64: pgx encodes nil as SQL NULL
			m.SurfaceType,   // 4-char per-wheel surface code; "" for pre-format-C frames
			m.Steering,      // radians, mean of front road-wheel angles; 0 for format-A
		}
	}
	if _, err := s.pool.CopyFrom(context.Background(),
		pgx.Identifier{"lap_trace"}, traceColumns, pgx.CopyFromRows(rows)); err != nil {
		return fmt.Errorf("processing: save trace batch (%d rows): %w", len(samples), err)
	}
	return nil
}

// ByLap returns a lap's samples ordered by seq. Read/scan errors surface (not masked as "no data").
func (s *PostgresTraceStore) ByLap(sessionID string, lapNumber int32) ([]Sample, error) {
	rows, err := s.pool.Query(context.Background(),
		"SELECT "+joinCols(traceColumns)+" FROM lap_trace WHERE session_id=$1 AND lap_number=$2 ORDER BY seq",
		sessionID, lapNumber)
	if err != nil {
		return nil, fmt.Errorf("processing: query trace %s#%d: %w", sessionID, lapNumber, err)
	}
	defer rows.Close()

	var out []Sample
	for rows.Next() {
		var (
			m   Sample
			seq int64
		)
		if err := rows.Scan(&m.RecvTime, &m.SessionID, &m.LapNumber, &seq, &m.Distance,
			&m.PosX, &m.PosY, &m.PosZ, &m.Speed, &m.Throttle, &m.Brake, &m.RPM, &m.Gear, &m.Yaw, &m.AngVelY,
			&m.Fuel, &m.TyreTempFL, &m.TyreTempFR, &m.TyreTempRL, &m.TyreTempRR, &m.Clutch, &m.Boost,
			&m.DeltaToBestNs, &m.SurfaceType, &m.Steering); err != nil { // *int64: SQL NULL scans back to nil
			return nil, fmt.Errorf("processing: scan trace row: %w", err)
		}
		m.Seq = uint64(seq)
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("processing: iterate trace rows: %w", err)
	}
	return out, nil
}

// DeleteSession removes a session's trace rows.
func (s *PostgresTraceStore) DeleteSession(sessionID string) error {
	if _, err := s.pool.Exec(context.Background(), "DELETE FROM lap_trace WHERE session_id=$1", sessionID); err != nil {
		return fmt.Errorf("processing: delete trace of %s: %w", sessionID, err)
	}
	return nil
}

// joinCols renders a column list for SELECT (COPY takes the slice form directly).
func joinCols(cols []string) string {
	s := ""
	for i, c := range cols {
		if i > 0 {
			s += ", "
		}
		s += c
	}
	return s
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
			&durNs, &fc, &gc, &lap.Complete, &lap.Timed, &lap.Valid, &lap.Fingerprint); err != nil {
			return out
		}
		lap.Duration = time.Duration(durNs)
		lap.FrameCount = uint64(fc)
		lap.GapCount = uint64(gc)
		out = append(out, &lap)
	}
	return out
}
