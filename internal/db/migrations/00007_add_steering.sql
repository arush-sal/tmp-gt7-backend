-- +goose Up
-- +goose StatementBegin
-- Front-wheel steering angle in RADIANS: the mean of the two front road-wheel angles (GT7 format-C
-- only). Served in the per-lap trace and the predicted/reference lap so the dashboard can graph
-- steering. NOT NULL DEFAULT 0 so pre-existing rows and format-A frames backfill to 0 (no data).
ALTER TABLE lap_trace
    ADD COLUMN steering REAL NOT NULL DEFAULT 0;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE lap_trace
    DROP COLUMN steering;
-- +goose StatementEnd
