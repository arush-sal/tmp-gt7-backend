import { useCallback, useEffect, useRef, useState } from "react";

/** lastIndexAtOrBefore returns the last index whose time is <= t (0 when t precedes all); times ascending. */
function lastIndexAtOrBefore(times: readonly number[], t: number): number {
  let lo = 0, hi = times.length - 1;
  while (lo < hi) {
    const mid = (lo + hi + 1) >> 1;
    if (times[mid] <= t) lo = mid; else hi = mid - 1;
  }
  return lo;
}

/**
 * usePlayback plays a lap back in real time (1x). `times` are the lap's per-sample times in seconds
 * (ascending); while playing, each animation frame advances a playhead and reports the sample index
 * at it via `onIndex` (the caller feeds that into the shared scrub cursor). Pause keeps the playhead;
 * reaching the end stops and the next Play restarts from 0. A new `times` (lap/session change) stops
 * and resets. Needs at least two samples.
 */
export function usePlayback(times: readonly number[] | null, onIndex: (i: number) => void) {
  const [playing, setPlaying] = useState(false);
  const head = useRef(0); // seconds since times[0]
  const onIndexRef = useRef(onIndex);
  onIndexRef.current = onIndex;
  const canPlay = !!times && times.length >= 2;

  // New lap: stop and rewind. Runs before the play effect below, so its cleanup cancels the loop.
  useEffect(() => { setPlaying(false); head.current = 0; }, [times]);

  useEffect(() => {
    if (!playing || !times || times.length < 2) return;
    const t0 = times[0], dur = times[times.length - 1] - t0;
    if (head.current >= dur) head.current = 0; // replay after the end starts over
    let last: number | null = null;
    let id = 0;
    const step = (now: number) => {
      if (last !== null) head.current += (now - last) / 1000;
      last = now;
      if (head.current >= dur) {
        head.current = dur;
        onIndexRef.current(times.length - 1);
        setPlaying(false);
        return;
      }
      onIndexRef.current(lastIndexAtOrBefore(times, t0 + head.current));
      id = requestAnimationFrame(step);
    };
    id = requestAnimationFrame(step);
    return () => cancelAnimationFrame(id);
  }, [playing, times]);

  const toggle = useCallback(() => { if (canPlay) setPlaying(p => !p); }, [canPlay]);
  return { playing, toggle, canPlay };
}
