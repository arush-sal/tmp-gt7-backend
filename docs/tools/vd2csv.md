# vd2csv

Decodes a [VictoryDash](https://victorydash.app) `telemetry.json` export — one driven GT7 lap — into
the same full-field CSV format as [pcap2csv](pcap2csv.md). It reuses the backend's packet decoder
(`collector/protocol`).

VictoryDash exports already contain **decrypted** 368-byte Format-C packets (as integer arrays under
`rawUdpPackets`), so decryption is skipped — the bytes go straight to the decoder.

## Build

```bash
go build ./tools/rkb/vd2csv
```

## Usage

```
vd2csv <telemetry.json> <out.csv>
```

Positional arguments only, no flags:

- `<telemetry.json>` — a VictoryDash replay export containing `rawUdpPackets` and `lapTimeMs`.
- `<out.csv>` — the CSV to write.

Example:

```bash
./vd2csv telemetry.json lap.csv
```

## What it does

- Parses the export's `rawUdpPackets` (an array of integer-encoded packets).
- Processes only 368-byte packets (the `fmt` column is always `368`); other lengths are skipped.
- De-duplicates consecutive packets by `PackageID`.
- Writes one CSV row per kept packet, using the **same column layout as `pcap2csv`**.

A summary line is printed to stderr when it finishes.
