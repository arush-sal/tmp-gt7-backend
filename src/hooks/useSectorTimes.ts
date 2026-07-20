// src/hooks/useSectorTimes.ts
import { useEffect, useRef, useState } from "react";
import type { LiveSnapshot } from "../contract/live";
import type { DrawGeometry } from "../lib/rkbGeometry";
import { SectorTimer, type SectorTime } from "../lib/sectorTimer";

const sectorKey = (trackKey: string) => `gt7:sectors:${trackKey}`;

// Best-effort restore of a track's sticky sector state after a page refresh.
function seedFromStorage(timer: SectorTimer, trackKey: string | null) {
  if (!trackKey || !timer.hasPartition()) return;
  try {
    const raw = localStorage.getItem(sectorKey(trackKey));
    if (!raw) return;
    const p = JSON.parse(raw);
    if (Array.isArray(p?.times)) timer.seedState(p.sessionKey ?? null, p.lap ?? null, p.times);
  } catch { /* absent / corrupt — leave the timer empty */ }
}

/**
 * Live sector-split timer wired to the current snapshot + the loaded track's geometry.
 * The partition (sector boundaries) is computed once per track -- `trackKey` (the track
 * slug) identifies when to recreate the timer; passing the same key keeps the running
 * state across snapshots. `null` whenever the timer has no usable partition (no cl, or
 * fewer than 2 sector labels) -- callers keep showing a "pending" placeholder in that case.
 */
export function useSectorTimes(
  snapshot: LiveSnapshot | null,
  geometry: DrawGeometry | null,
  trackKey: string | null,
  live = true,
): SectorTime[] | null {
  const holder = useRef<{ key: string | null; timer: SectorTimer } | null>(null);

  // `trackKey` (the slug) is set as soon as auto-detection picks a track, but
  // `useTrack` loads the actual geometry record asynchronously -- so on the very
  // first render(s) for a given trackKey, `geometry` is still null and the timer
  // built from it has no partition. Rebuild whenever trackKey changes (fresh track,
  // reset running state) OR whenever the current timer still has no partition but
  // the incoming geometry now looks usable (non-empty centerline + >=2 sector
  // labels) -- i.e. geometry has arrived since the timer was built. Once a
  // partitioned timer exists for the current trackKey, leave it alone so running
  // state survives across snapshots.
  const geometryUsable = !!geometry && geometry.cl.length > 0 && geometry.sectors.length >= 2;
  const needsRebuild =
    !holder.current ||
    holder.current.key !== trackKey ||
    (!holder.current.timer.hasPartition() && geometryUsable);

  if (needsRebuild) {
    const timer = new SectorTimer(geometry);
    seedFromStorage(timer, trackKey); // restore sticky sectors across a refresh
    holder.current = { key: trackKey, timer };
  }

  const [times, setTimes] = useState<SectorTime[] | null>(null);
  const timer = holder.current!.timer;
  const lastPersisted = useRef<string>("");

  useEffect(() => {
    if (!live) return;
    if (!timer.hasPartition()) return;
    const f = snapshot?.frame;
    const d = snapshot?.derived;
    if (!f || !d) return;
    const { position_x: x, position_z: z } = f;
    if (x === undefined || z === undefined) return;
    const sessionKey = snapshot!.enrichment.session_id ?? trackKey;
    const result = timer.update({
      sessionKey,
      lap: f.current_lap,
      lapTimeNs: d.current_lap_time_ns,
      x,
      z,
    });
    setTimes(result);

    // Persist only when the displayed sectors actually change (a handful of times per lap),
    // so a refresh can restore them. Keyed by track so each track keeps its own splits.
    if (trackKey) {
      const payload = JSON.stringify({ sessionKey, lap: f.current_lap, times: result });
      if (payload !== lastPersisted.current) {
        try { localStorage.setItem(sectorKey(trackKey), payload); lastPersisted.current = payload; } catch { /* best effort */ }
      }
    }
    // `timer` is included so a mid-flight rebuild (geometry arriving after the initial
    // no-partition timer was built, on the same trackKey) re-runs against the new,
    // partitioned timer immediately -- not just on the next snapshot/trackKey change.
  }, [snapshot, trackKey, timer, live]);

  return timer.hasPartition() ? times : null;
}
