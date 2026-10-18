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
// laps.fingerprint (migration 00008) only constrains real fingerprints; an empty track slug/confidence
// is stored as NULL (no track guess). A re-save never erases a stored fingerprint (COALESCE): it is the
// lap's dedup key, and losing it lets a re-ingest of the same capture be stored as a new session.
const lapUpsertSQL = `
INSERT INTO laps
 (session_id, lap_number, start_time, end_time, duration_ns, frame_count, gap_count, complete, timed, valid, fingerprint,
  track_slug, track_confidence)
 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,NULLIF($11,''),NULLIF($12,''),NULLIF($13,''))
 ON CONFLICT (session_id, lap_number) DO UPDATE SET
   start_time=EXCLUDED.start_time, end_time=EXCLUDED.end_time, duration_ns=EXCLUDED.duration_ns,
   frame_count=EXCLUDED.frame_count, gap_count=EXCLUDED.gap_count, complete=EXCLUDED.complete,
   timed=EXCLUDED.timed, valid=EXCLUDED.valid, fingerprint=COALESCE(EXCLUDED.fingerprint, laps.fingerprint),
   track_slug=EXCLUDED.track_slug, track_confidence=EXCLUDED.track_confidence`

// Save upserts a lap. Duration stores as nanoseconds; counts widen to bigint (CHECK (>= 0) in the DB).
func (s *PostgresLapStore) Save(lap *Lap) error {
	_, err := s.pool.Exec(context.Background(), lapUpsertSQL,
		lap.SessionID, lap.LapNumber, lap.StartTime, lap.EndTime, lap.Duration.Nanoseconds(),
		int64(lap.FrameCount), int64(lap.GapCount), lap.Complete, lap.Timed, lap.Valid, lap.Fingerprint,
		lap.TrackSlug, lap.TrackConfidence)
	if pgErr := (*pgconn.PgError)(nil); errors.As(err, &pgErr) && pgErr.ConstraintName == "laps_fingerprint_key" {
		// The single INSERT failed atomically, so nothing was half-written; let the caller retry.
		return fmt.Errorf("processing: save lap %s#%d: %w", lap.SessionID, lap.LapNumber, ErrDuplicateFingerprint)
	}
	if err != nil {
		return fmt.Errorf("processing: save lap %s#%d: %w", lap.SessionID, lap.LapNumber, err)
	}
	return nil
}

const lapCols = `session_id, lap_number, start_time, end_time, duration_ns, frame_count, gap_count, complete, timed, valid, COALESCE(fingerprint, ''),
 COALESCE(track_slug, ''), COALESCE(track_confidence, ''),
 COALESCE((SELECT total_laps FROM sessions WHERE sessions.id = laps.session_id), 0)`

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

// DominantTracks implements LapStore with one grouped query: per session, the confident track with the
// most laps, ties broken by the earliest lap start, then by slug (deterministic).
// ponytail: aggregates every confident lap on each /v1/sessions call; fine for a personal history (~100s
// of laps). If it grows, add a partial index on laps(session_id, track_slug) WHERE confident, or keep a
// per-session summary.
func (s *PostgresLapStore) DominantTracks() map[string]string {
	rows, err := s.pool.Query(context.Background(), `
SELECT DISTINCT ON (session_id) session_id, track_slug FROM (
  SELECT session_id, track_slug, count(*) AS n, min(start_time) AS first
  FROM laps WHERE track_confidence='confident' AND track_slug <> ''
  GROUP BY session_id, track_slug) g
ORDER BY session_id, n DESC, first ASC, track_slug ASC`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := make(map[string]string)
	for rows.Next() {
		var id, slug string
		if rows.Scan(&id, &slug) == nil {
			out[id] = slug
		}
	}
	return out
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

// LabelTrack implements TrackRefStore (one UPDATE; runs once per detector lock). A zero span.Until is
// open-ended (a NULL bound).
func (s *PostgresLapStore) LabelTrack(ctx context.Context, sessionID string, span TimeSpan, slug string) error {
	var until *time.Time
	if !span.Until.IsZero() {
		until = &span.Until
	}
	_, err := s.pool.Exec(ctx, `UPDATE laps SET track_slug=$3, track_confidence='confident'
 WHERE session_id=$1 AND start_time >= $2 AND ($4::timestamptz IS NULL OR start_time < $4)
   AND (track_confidence IS NULL OR track_confidence='provisional')`,
		sessionID, span.From, slug, until)
	if err != nil {
		return fmt.Errorf("processing: label laps of %s as %s: %w", sessionID, slug, err)
	}
	return nil
}

// ClearProvisionalLabels implements TrackRefStore. One UPDATE, once per session end; laps that started
// inside a keep span are skipped (the spans travel as two parallel timestamptz arrays, a null Until
// being open-ended).
func (s *PostgresLapStore) ClearProvisionalLabels(ctx context.Context, sessionID string, keep ...TimeSpan) error {
	from := make([]time.Time, len(keep))
	until := make([]*time.Time, len(keep))
	for i, k := range keep {
		from[i] = k.From
		if !k.Until.IsZero() {
			u := k.Until
			until[i] = &u
		}
	}
	_, err := s.pool.Exec(ctx, `UPDATE laps SET track_slug=NULL, track_confidence=NULL
 WHERE session_id=$1 AND track_confidence='provisional'
   AND NOT EXISTS (SELECT 1 FROM unnest($2::timestamptz[], $3::timestamptz[]) AS k(f, u)
                   WHERE laps.start_time >= k.f AND (k.u IS NULL OR laps.start_time < k.u))`,
		sessionID, from, until)
	if err != nil {
		return fmt.Errorf("processing: clear provisional labels of %s: %w", sessionID, err)
	}
	return nil
}

// SetSessionTrack implements TrackRefStore. One UPDATE per user pick (or its withdrawal).
func (s *PostgresLapStore) SetSessionTrack(ctx context.Context, sessionID, slug, confidence string) error {
	if _, err := s.pool.Exec(ctx, `UPDATE laps SET track_slug=$2, track_confidence=$3 WHERE session_id=$1`,
		sessionID, slug, confidence); err != nil {
		return fmt.Errorf("processing: set track of %s to %s/%s: %w", sessionID, slug, confidence, err)
	}
	return nil
}

// Lap implements TrackRefStore.
func (s *PostgresLapStore) Lap(sessionID string, lapNumber int32) (*Lap, bool, error) {
	return s.oneLap("SELECT "+lapCols+" FROM laps WHERE session_id=$1 AND lap_number=$2", sessionID, lapNumber)
}

// oneLap runs a query returning at most one lap row.
func (s *PostgresLapStore) oneLap(sql string, args ...any) (*Lap, bool, error) {
	rows, err := s.pool.Query(context.Background(), sql, args...)
	if err != nil {
		return nil, false, fmt.Errorf("processing: query lap: %w", err)
	}
	defer rows.Close()
	laps := scanLaps(rows)
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("processing: read lap: %w", err)
	}
	if len(laps) == 0 {
		return nil, false, nil
	}
	return laps[0], true, nil
}

// TrackLaps implements TrackRefStore. The WHERE clause matches the laps_track_fastest partial index
// (migration 00009), which also serves the ORDER BY; the race-lap-1 exclusion (RefEligible) is a
// filter on top of that index scan, so it needs no new index or migration.
func (s *PostgresLapStore) TrackLaps(slug string, limit int) ([]*Lap, error) {
	rows, err := s.pool.Query(context.Background(), "SELECT "+lapCols+` FROM laps
 WHERE track_slug=$1 AND complete AND valid AND track_confidence='confident'
 AND NOT (lap_number = 1 AND EXISTS (SELECT 1 FROM sessions s WHERE s.id = laps.session_id AND s.total_laps > 0))
 ORDER BY duration_ns, start_time LIMIT $2`, slug, limit)
	if err != nil {
		return nil, fmt.Errorf("processing: track laps %s: %w", slug, err)
	}
	defer rows.Close()
	laps := scanLaps(rows)
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("processing: read track laps %s: %w", slug, err)
	}
	return laps, nil
}

// PinnedReference implements TrackRefStore.
func (s *PostgresLapStore) PinnedReference(slug string) (*Lap, bool, error) {
	return s.oneLap("SELECT "+lapCols+` FROM laps WHERE (session_id, lap_number) =
 (SELECT session_id, lap_number FROM track_reference WHERE track_slug=$1)`, slug)
}

// PinReference implements TrackRefStore (upsert on the slug). A missing lap trips the FK.
func (s *PostgresLapStore) PinReference(slug, sessionID string, lapNumber int32) error {
	_, err := s.pool.Exec(context.Background(), `INSERT INTO track_reference (track_slug, session_id, lap_number)
 VALUES ($1,$2,$3) ON CONFLICT (track_slug) DO UPDATE SET session_id=EXCLUDED.session_id,
 lap_number=EXCLUDED.lap_number, pinned_at=now()`, slug, sessionID, lapNumber)
	if pgErr := (*pgconn.PgError)(nil); errors.As(err, &pgErr) && pgErr.Code == "23503" { // foreign_key_violation
		return fmt.Errorf("processing: pin %s#%d: %w", sessionID, lapNumber, ErrLapNotFound)
	}
	if err != nil {
		return fmt.Errorf("processing: pin %s#%d for %s: %w", sessionID, lapNumber, slug, err)
	}
	return nil
}

// UnpinReference implements TrackRefStore.
func (s *PostgresLapStore) UnpinReference(slug string) (bool, error) {
	tag, err := s.pool.Exec(context.Background(), "DELETE FROM track_reference WHERE track_slug=$1", slug)
	if err != nil {
		return false, fmt.Errorf("processing: unpin %s: %w", slug, err)
	}
	return tag.RowsAffected() > 0, nil
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
			&durNs, &fc, &gc, &lap.Complete, &lap.Timed, &lap.Valid, &lap.Fingerprint,
			&lap.TrackSlug, &lap.TrackConfidence, &lap.TotalLaps); err != nil {
			return out
		}
		lap.Duration = time.Duration(durNs)
		lap.FrameCount = uint64(fc)
		lap.GapCount = uint64(gc)
		out = append(out, &lap)
	}
	return out
}

// UnpinReferenceIf implements TrackRefStore.
func (s *PostgresLapStore) UnpinReferenceIf(slug, sessionID string, lapNumber int32) (bool, error) {
	tag, err := s.pool.Exec(context.Background(),
		"DELETE FROM track_reference WHERE track_slug=$1 AND session_id=$2 AND lap_number=$3", slug, sessionID, lapNumber)
	if err != nil {
		return false, fmt.Errorf("processing: conditional unpin %s: %w", slug, err)
	}
	return tag.RowsAffected() > 0, nil
}
