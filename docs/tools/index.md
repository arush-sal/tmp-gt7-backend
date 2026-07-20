# Tools

The backend ships a set of developer and data-pipeline tools. Two are small Go decoders that turn
captured telemetry into CSV; one is an offline Python pipeline that builds the Reference Knowledge
Base (RKB) the backend serves.

| Tool | Language | Purpose | Page |
|---|---|---|---|
| `pcap2csv` | Go | Decode a `.pcapng` GT7 capture to a full-field CSV. | [pcap2csv](pcap2csv.md) |
| `vd2csv` | Go | Decode a VictoryDash replay export to a full-field CSV. | [vd2csv](vd2csv.md) |
| `rkb` | Python | Build the offline Track RKB (`rkb.db`, index, per-track JSON). | [rkb](rkb.md) |

## Where they live

All three live under `tools/rkb/`. The two Go decoders reuse the backend's own packet-decode code
(`collector/protocol`) so the wire format is decoded exactly once, in one place. The Python pipeline
is a self-contained package (`tools/rkb/rkb/`) that consumes the CSVs those decoders produce, among
other sources.

## Data flow

```
.pcapng  --pcap2csv-->  CSV  --.
                                \
VictoryDash --vd2csv-->  CSV  ---+--> rkb (Python) --> out/rkb.db + rkb-index.csv + out/rkb/*.json
                                /
KB SVGs + course/detect CSVs --'
```

The backend serves `rkb.db` (copied to `$HOME/.garage/rkb.db`) via its
[track API](../api.md#the-rkb-database).
