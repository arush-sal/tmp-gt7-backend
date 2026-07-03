-- +goose Up
-- +goose StatementBegin
-- sessions: one row per detected on-track stint (Session Management domain 2).
-- Plain relational table (not a TimescaleDB hypertable) — sessions are discrete records, not a
-- time-series; telemetry time-series/laps get Timescale later. Column types widen to Postgres
-- integer/bigint (no uint); durations stored as nanoseconds (bigint); end_time is NULL while the
-- session is still active (GT7 lap fields go to -1 in menu frames, so end fields come from the last
-- in-race frame — see the session.Record doc).
CREATE TABLE IF NOT EXISTS sessions (
    id               TEXT        PRIMARY KEY,
    start_time       TIMESTAMPTZ NOT NULL,
    end_time         TIMESTAMPTZ,
    car_id           INTEGER     NOT NULL,
    source           INTEGER     NOT NULL,   -- telemetryv1.Source enum value (LIVE/REPLAY)
    -- These come from Go unsigned types (uint64 counts, uint32 package ids). CHECK (>= 0) guards
    -- against corrupt/external writes so a negative bigint can never scan back into an unsigned Go
    -- field as a wrapped value (defense-in-depth; our own writes are always non-negative).
    frame_count      BIGINT      NOT NULL CHECK (frame_count >= 0),
    gap_count        BIGINT      NOT NULL CHECK (gap_count >= 0),
    first_package_id BIGINT      NOT NULL CHECK (first_package_id >= 0),   -- GT7 uint32 widened
    last_package_id  BIGINT      NOT NULL CHECK (last_package_id >= 0),
    current_lap      INTEGER     NOT NULL,
    total_laps       INTEGER     NOT NULL,
    best_lap_ns      BIGINT      NOT NULL,   -- time.Duration nanoseconds; 0 when unset
    last_lap_ns      BIGINT      NOT NULL,
    status           TEXT        NOT NULL,   -- active | finished | abandoned
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS sessions;
-- +goose StatementEnd
