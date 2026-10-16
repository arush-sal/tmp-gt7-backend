// A real GET /v1/live snapshot captured 2026-07-06, used as test/dev sample data.
// Transcribed from ../sample-json-output.json (JSONC comment removed). Typed `unknown`
// so tests can feed it through validateLive exactly as the wire would deliver it.
export const liveSample: unknown = {
  served_at: "2026-07-06T11:28:30.007218892Z",
  status: "live",
  frame: {
    recv_timestamp: "2026-07-06T11:28:30.006595216Z", sequence: "42315", gap: false,
    source: "SOURCE_LIVE", schema_version: 1, package_id: 1258714, car_id: 3438,
    current_lap: 77, total_laps: 90, best_lap: "68.602s", last_lap: "76.237s",
    current_position: 1, total_positions: 20, time_on_track: "62520s",
    rpm: 6629, rpm_rev_warning: 8300, rpm_rev_limiter: 9000, rpm_after_clutch: 6629,
    estimated_top_speed: 255, current_gear: 5, suggested_gear: 15, clutch: 0, clutch_engaged: 1,
    boost: -0.25926256, throttle: 68.62745, brake: 0, car_speed: 183.84972,
    fuel_capacity: 100, current_fuel: 100,
    position_x: -311.14505, position_y: 7.143566, position_z: 718.0311,
    velocity_x: 51.077126, velocity_y: -0.072092324, velocity_z: 1.6499143,
    rotation_pitch: -0.17826372, rotation_yaw: -0.6874388, rotation_roll: 0.1948584,
    angular_velocity_x: 0.1728218, angular_velocity_y: 0.16432315, angular_velocity_z: -0.07723996,
    ride_height: 98.236084,
    tyre_diameter_fl: 0.2539, tyre_diameter_fr: 0.2539, tyre_diameter_rl: 0.2539, tyre_diameter_rr: 0.2539,
    tyre_speed_fl: 183.5875, tyre_speed_fr: 184.52534, tyre_speed_rl: 191.84435, tyre_speed_rr: 191.66075,
    tyre_slip_ratio_fl: 0.9985737, tyre_slip_ratio_fr: 1.0036749, tyre_slip_ratio_rl: 1.0434846, tyre_slip_ratio_rr: 1.042486,
    tyre_temp_fl: 42.15736, tyre_temp_fr: 30.689157, tyre_temp_rl: 86.77724, tyre_temp_rr: 80.08314,
    suspension_fl: 0.11799345, suspension_fr: 0.11679308, suspension_rl: 0.17813464, suspension_rr: 0.17854987,
    oil_temp: 110, water_temp: 85, oil_pressure: 6.6841507,
    gear1: 1.9850001, gear2: 1.3790001, gear3: 1.016, gear4: 0.78900003, gear5: 0.646, gear6: 0, gear7: 0, gear8: 0,
    in_race: true, is_paused: false, is_loading: false, is_in_gear: true, car_has_turbo: true,
    is_rev_limiter_flashing: false, is_handbrake_engaged: false, is_lights_on: false,
    is_low_beam_on: false, is_high_beam_on: false, is_asm_engaged: false, is_tcs_engaged: true,
  },
  derived: { current_lap_time_ns: 21794021701, fuel_pct: 100, source: "SOURCE_LIVE" },
  enrichment: {
    session_id: "sess-1",
    analysis: {
      session_id: "sess-1", has_pb: true, pb_lap: 0, pb_time_ns: 68602000000,
      laps: [
        { lap_number: 70, duration_ns: 77276000000, delta_to_pb_ns: 8674000000, sector_deltas: null },
        { lap_number: 76, duration_ns: 76237000000, delta_to_pb_ns: 7635000000, sector_deltas: null },
      ],
      consistency: { count: 7, best_ns: 75584000000, mean_ns: 76311285714, std_dev_ns: 505990320, spread_ns: 1692000000 },
      predicted_best_ns: 0, time_on_table_ns: 0, best_sectors: null,
      recommendations: ["Personal Best: 1:08.602."],
    },
    car_name: null, track: null, delta_to_best_ns: 61555907,
  },
};
