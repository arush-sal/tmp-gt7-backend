-- +goose Up
-- +goose StatementBegin
-- Track per LAP (not per session): a session can drive several tracks, so each lap carries the track
-- the live detector had when the lap completed, and its confidence ('provisional' | 'confident').
-- NULL = no track guess at that moment (or a pre-migration row). A lock backfills the laps of its
-- detection stint that were unlabelled or provisional (see processing.TrackDetector.OnLock).
ALTER TABLE laps
    ADD COLUMN track_slug       TEXT,
    ADD COLUMN track_confidence TEXT;
-- +goose StatementEnd

-- +goose StatementBegin
-- Serves "fastest valid complete confident laps on track S" (the stored-reference fallback and
-- GET /v1/tracks/{slug}/laps). Partial: only reference-eligible laps are indexed.
CREATE INDEX IF NOT EXISTS laps_track_fastest ON laps (track_slug, duration_ns)
    WHERE complete AND valid AND track_confidence = 'confident';
-- +goose StatementEnd

-- +goose StatementBegin
-- car_category: GT7 format-C car category code (e.g. "GR4"), snapshotted at session open. NULL for
-- format-A telemetry (no category) and pre-migration rows.
ALTER TABLE sessions ADD COLUMN car_category TEXT;
-- +goose StatementEnd

-- +goose StatementBegin
-- track_reference: the user's pinned reference lap per track (at most one per slug). The FK cascades
-- from laps, so deleting or discarding the pinned lap's session removes the pin and the track falls
-- back to its fastest stored lap.
CREATE TABLE IF NOT EXISTS track_reference (
    track_slug TEXT        PRIMARY KEY,
    session_id TEXT        NOT NULL,
    lap_number INTEGER     NOT NULL,
    pinned_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (session_id, lap_number) REFERENCES laps (session_id, lap_number) ON DELETE CASCADE
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS track_reference;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE sessions DROP COLUMN car_category;
-- +goose StatementEnd

-- +goose StatementBegin
DROP INDEX IF EXISTS laps_track_fastest;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE laps
    DROP COLUMN track_slug,
    DROP COLUMN track_confidence;
-- +goose StatementEnd
