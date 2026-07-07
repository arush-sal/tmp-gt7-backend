-- +goose Up
-- +goose StatementBegin
-- car_name: human-readable car name, resolved by the collector at the frame seam and snapshotted
-- onto the session at open. Added after 00001. DEFAULT 'Unknown' backfills any pre-existing rows and
-- matches the collector's own fallback for an unmapped car_id, so no session ever renders a blank car.
ALTER TABLE sessions ADD COLUMN car_name TEXT NOT NULL DEFAULT 'Unknown';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE sessions DROP COLUMN car_name;
-- +goose StatementEnd
