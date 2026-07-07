import type { LiveSnapshot, Frame, Derived, Enrichment } from "../contract/live";

export type Channel = "throttle" | "brake" | "clutch" | "rpm" | "gear" | "speed" | "boost" | "delta";

type Sample = { t: number } & Record<Channel, number>; // t = seconds since lap start; delta may be NaN

function project(f: Frame, d: Derived, e: Enrichment): Sample {
  return {
    t: d.current_lap_time_ns / 1e9,
    throttle: f.throttle, brake: f.brake, clutch: f.clutch, rpm: f.rpm,
    gear: f.current_gear, speed: f.car_speed, boost: f.boost,
    delta: e.delta_to_best_ns === null ? NaN : e.delta_to_best_ns / 1e9,
  };
}

export type HistoryStore = {
  push(s: LiveSnapshot): void;
  columns(chs: Channel[]): number[][];
  size(): number;
  reset(): void;
};

export function createHistoryStore(capMs = 120_000): HistoryStore {
  let buf: Sample[] = [];
  let lap: number | null = null;
  let session: string | undefined;
  const capS = capMs / 1000;
  return {
    push(s) {
      if (s.status !== "live" || !s.frame || !s.derived) return;
      const sid = s.enrichment.session_id;
      if (sid !== session) { session = sid; buf = []; lap = null; }
      if (lap !== null && s.frame.current_lap !== lap) buf = []; // lap rollover
      lap = s.frame.current_lap;
      const sample = project(s.frame, s.derived, s.enrichment);
      // x must be strictly increasing (uPlot's aligned-data contract + the eviction loop's sorted
      // assumption). Drop a regressed/duplicate same-lap timestamp rather than corrupt the series.
      if (buf.length && sample.t <= buf[buf.length - 1].t) return;
      buf.push(sample);
      const cutoff = buf[buf.length - 1].t - capS;
      while (buf.length > 1 && buf[0].t < cutoff) buf.shift();
    },
    columns(chs) {
      const xs = buf.map(s => s.t);
      return [xs, ...chs.map(c => buf.map(s => s[c]))];
    },
    size() { return buf.length; },
    reset() { buf = []; lap = null; session = undefined; },
  };
}
