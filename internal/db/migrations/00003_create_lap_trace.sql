-- +goose Up
-- +goose StatementBegin
-- Per-frame telemetry trace (Processing v2), one row per canonical frame received for a lap. This is
-- genuine time-series data, so it is a TimescaleDB hypertable partitioned on recv_timestamp (D-B15).
-- distance is the cumulative in-lap distance computed at ingest (running sum of position deltas);
-- normalize 0..1 on read. Full fidelity — no downsampling (decimate at read time for display).
-- FK to sessions(id) ON DELETE CASCADE: this is a hypertable -> regular-table FK (the supported
-- direction), giving orphan-free integrity + cascade cleanup (reviewed with Codex — see decision log).
CREATE EXTENSION IF NOT EXISTS timescaledb;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS lap_trace (
    recv_timestamp TIMESTAMPTZ       NOT NULL,
    session_id     TEXT              NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    lap_number     INTEGER           NOT NULL,
    seq            BIGINT            NOT NULL CHECK (seq >= 0), -- collector's monotonic frame counter
    distance       DOUBLE PRECISION  NOT NULL CHECK (distance >= 0), -- cumulative in-lap metres
    pos_x          REAL              NOT NULL,
    pos_y          REAL              NOT NULL,
    pos_z          REAL              NOT NULL,
    speed          REAL              NOT NULL, -- km/h
    throttle       REAL              NOT NULL, -- 0-100
    brake          REAL              NOT NULL, -- 0-100
    rpm            REAL              NOT NULL,
    gear           INTEGER           NOT NULL,
    yaw            REAL              NOT NULL, -- rotation yaw (for corner detection)
    ang_vel_y      REAL              NOT NULL  -- yaw-rate proxy (for corner detection)
);
-- +goose StatementEnd

-- +goose StatementBegin
SELECT create_hypertable('lap_trace', 'recv_timestamp', if_not_exists => TRUE);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS lap_trace_session_lap_seq ON lap_trace (session_id, lap_number, seq);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS lap_trace;
-- +goose StatementEnd
