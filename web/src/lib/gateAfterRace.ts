import type { LiveSource } from "./liveSource";

// Stream latch that stops all downstream processing once a race is over.
//
// GT7 ticks current_lap to N+1 the instant you cross the line finishing lap N, so the first
// frame with current_lap > total_laps IS the finalizer of the last real lap — it must reach the
// consumers (it completes that lap in historyStore, freezes the sticky view, flips `live` off).
// Every cooldown frame after it is noise: it would spawn phantom laps, re-detect the track, and
// keep the "live" dot lit. So: forward that ONE finalizer, then drop everything until a new race
// brings current_lap back in bounds (which unlatches and forwards again).
//
// Wrapping the shared LiveSource means a single latch governs history, sectors, view, and track
// together. Time trials (total_laps === 0) never overflow, so the gate is a transparent no-op.
//
// ponytail: latch is per-subscription — useSession and useHistory each wrap independently, so
// their latches aren't synchronized. Harmless today (both see the same finalizer frame); the real
// fix is the deferred single-subscription fan-out, not tighter coupling here.
export function gateAfterRace(source: LiveSource): LiveSource {
  return (url, handlers) => {
    let ended = false;
    return source(url, {
      ...handlers,
      onSnapshot: (s) => {
        const f = s.frame;
        const overflow = !!f && f.total_laps > 0 && f.current_lap > f.total_laps;
        if (ended) {
          if (overflow) return;   // still the finished race's cooldown -> drop
          ended = false;          // back in bounds -> new race -> forward below
        }
        handlers.onSnapshot(s);
        if (overflow) ended = true; // latch AFTER forwarding the finalizer frame
      },
    });
  };
}
