// src/contract/live.ts
// Wire shape of GET /v1/live and each /v1/live/stream event. snake_case = the wire.
// Verified against ../backend/rtps/live/payload.go + enrichment.go (2026-07-07).

export type LiveStatus = "live" | "idle";

// Only the fields this slice reads are listed. The backend emits every frame field
// (protojson EmitUnpopulated); validateLive ignores unknown extras.
export type Frame = {
  car_speed: number;
  current_gear: number;
  suggested_gear: number;
  rpm: number;
  throttle: number;          // 0..100 (raw 0-255 / 2.55)
  brake: number;             // 0..100 (raw 0-255 / 2.55)
  clutch: number;            // 0..1 (native float32)
  boost: number;
  current_fuel: number;
  fuel_capacity: number;
  tyre_temp_fl: number;      // degC
  tyre_temp_fr: number;
  tyre_temp_rl: number;
  tyre_temp_rr: number;
  current_lap: number;
  total_laps: number;
  current_position: number;
  total_positions: number;
  best_lap: string | null;   // protobuf Duration "77.210s" | null
  last_lap: string | null;
  in_race: boolean;
  is_paused: boolean;

  // --- GT7 "format-C" fields (frontend-handoff.md). OPTIONAL: absent on format-A / older
  // sources; validateLive includes each only when present with the right type. Confidence tiers
  // per the handoff — only the CONFIRMED group is safe to display as-is.
  // CONFIRMED
  current_lap_time_ms?: number;               // ms — raw packet lap clock. NOT ns; do not divide.
  wheel_steering_angle_front_left?: number;   // rad (front road wheel)
  wheel_steering_angle_front_right?: number;  // rad
  wheel_base?: number;                        // m — per-car constant (axle-to-axle)
  throttle_filtered?: number;                 // 0..255 filtered throttle
  surface_type?: string;                      // per-wheel FL,FR,RL,RR codes, e.g. "TTTT"
  car_category?: string;                      // GT7 category code, e.g. "GRN"
  // PLAUSIBLE
  wheel_rotation?: number;                    // rad — hand-wheel angle, clamped ±π
  steering_angular_velocity?: number;         // rad/s
  sway?: number;                              // lateral (X) body accel
  heave?: number;                             // vertical (Y) body accel
  surge?: number;                             // longitudinal (Z) body accel
  // UNCONFIRMED — read 0/constant in every capture to date
  brake_filtered?: number;                    // 0..255
  torque_vector_fl?: number;
  torque_vector_fr?: number;
  torque_vector_rl?: number;
  torque_vector_rr?: number;
  energy_recovery?: number;
  // UNKNOWN — meaning not identified; do not surface
  unknown_u8_13e?: number;
  unknown_u8_13f?: number;
  unknown_f32_154?: number;
};

export type Derived = {
  current_lap_time_ns: number;
  fuel_pct: number;          // 0..100
  source: string;
};

export type Enrichment = {
  session_id?: string;              // omitempty -> optional
  analysis: unknown | null;         // SessionAnalysis; not deep-validated this slice
  car_name: string | null;
  track: string | null;
  delta_to_best_ns: number | null;  // ns; null until a reference lap; -ve = ahead
};

export type LiveSnapshot = {
  served_at: string;                // RFC3339Nano
  status: LiveStatus;
  frame: Frame | null;              // null iff idle
  derived: Derived | null;          // null iff idle
  enrichment: Enrichment;
};
