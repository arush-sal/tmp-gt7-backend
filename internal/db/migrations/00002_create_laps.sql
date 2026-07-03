-- +goose Up
-- +goose StatementBegin
-- laps: the segmented laps of a session (Processing domain 3). One row per (session, lap number),
-- FK to sessions with cascade delete. duration_ns stores time.Duration nanoseconds; counts widen
-- from Go unsigned types with CHECK (>= 0) guards (same rationale as sessions). complete/timed/valid
-- carry the segmenter's classification (valid = timed && complete && no gaps = Personal-Best usable).
CREATE TABLE IF NOT EXISTS laps (
    session_id  TEXT        NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    lap_number  INTEGER     NOT NULL,
    start_time  TIMESTAMPTZ NOT NULL,
    end_time    TIMESTAMPTZ NOT NULL,
    duration_ns BIGINT      NOT NULL CHECK (duration_ns >= 0),
    frame_count BIGINT      NOT NULL CHECK (frame_count >= 0),
    gap_count   BIGINT      NOT NULL CHECK (gap_count >= 0),
    complete    BOOLEAN     NOT NULL,
    timed       BOOLEAN     NOT NULL,
    valid       BOOLEAN     NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (session_id, lap_number)
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS laps;
-- +goose StatementEnd
