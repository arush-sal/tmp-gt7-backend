// provisional: the slug follows the backend's auto-detected guess while it is still "provisional"
// (it may switch or be withdrawn); it turns false when the backend locks the track or the user picks.
export type SelState = { slug: string | null; locked: boolean; manual: boolean; startLap: number | null; autoClosed: boolean; provisional: boolean };
export const initialSel: SelState = { slug: null, locked: false, manual: false, startLap: null, autoClosed: false, provisional: false };

export function reduceManual(s: SelState, slug: string): SelState {
  return { slug, locked: true, manual: true, startLap: s.startLap, autoClosed: s.autoClosed, provisional: false };
}

// reduceAuto folds one snapshot's detected track into the selection. `provisional` is true when the
// backend reports track_confidence "provisional": the guess is shown but keeps following the backend
// (switches, withdrawals) until it locks, even past the first lap; a confident detection locks.
export function reduceAuto(s: SelState, detected: string | null, currentLap: number, moving: boolean, provisional = false): SelState {
  if (s.locked || s.manual) return s;                 // never override a lock/manual pick
  if (s.provisional) {                                 // following the backend's guess until it locks
    if (detected === s.slug && (provisional || detected === null)) return s; // unchanged: same object
    return { ...s, slug: detected, locked: detected !== null && !provisional, provisional: detected === null || provisional };
  }
  if (s.autoClosed) {
    // The first-lap window is closed, but a late detection may still fill an EMPTY selection (the
    // backend can need ~a lap to resolve); a shown slug (restored or picked) is never replaced.
    if (s.slug !== null || !detected) return s;
    return { ...s, slug: detected, locked: !provisional, provisional };
  }
  let startLap = s.startLap;
  if (startLap === null) {
    if (!moving) return s;                             // wait for first motion
    startLap = currentLap;                             // window opens
  }
  if (currentLap > startLap) {
    // first observed lap has completed -> permanently latch closed, no detection this call
    return { ...s, startLap, autoClosed: true };
  }
  // window still open (currentLap <= startLap)
  if (detected) return { slug: detected, locked: !provisional, manual: false, startLap, autoClosed: false, provisional };
  return { ...s, startLap };
}
