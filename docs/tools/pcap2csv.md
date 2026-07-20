# pcap2csv

Decodes a captured GT7 `.pcapng` file into a full-field CSV, one row per telemetry packet. It reuses
the backend's Salsa20 decrypt and packet decoder (`collector/protocol`), so decoding matches the
live collector exactly. Link-layer parsing is skipped — it reads packet payloads straight out of the
pcapng blocks.

## Build

```bash
go build ./tools/rkb/pcap2csv
```

## Usage

```
pcap2csv <in.pcapng> <out.csv>
```

Positional arguments only, no flags:

- `<in.pcapng>` — the captured GT7 UDP telemetry file.
- `<out.csv>` — the CSV to write.

Example:

```bash
./pcap2csv capture.pcapng capture.csv
```

## What it does

- Reads Enhanced (`0x06`) and Simple (`0x03`) packet blocks from the pcapng, handling either byte
  order.
- For each payload, decrypts and tries the known packet lengths largest-first — `368` (Format C),
  `344` (tilde), `296` (Format A) — and keeps the first that has a valid GT7 magic.
- Skips consecutive packets with the same `PackageID` (de-duplication).
- Writes one CSV row per kept packet.

The extended Format-C fields (steering, sway, surge, per-wheel surface type, and so on) are only
populated for 368-byte packets; on shorter packets those columns are zero/empty.

## Output columns

```
package_id, fmt, current_lap, in_race, time_on_track_ms,
pos_x, pos_y, pos_z, vel_x, vel_y, vel_z,
rot_pitch, rot_yaw, rot_roll, angvel_x, angvel_y, angvel_z,
car_speed, throttle, brake, gear,
slip_fl, slip_fr, slip_rl, slip_rr,
wheel_steer_l, wheel_steer_r, wheel_base, steer_ang_vel, wheel_rotation,
sway, heave, surge, throttle_filtered, brake_filtered,
surface_type, car_category, current_lap_time_ms
```

A summary line is printed to stderr when it finishes.
