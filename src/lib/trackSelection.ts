export type SelState = { slug: string | null; locked: boolean; manual: boolean; startLap: number | null; autoClosed: boolean };
export const initialSel: SelState = { slug: null, locked: false, manual: false, startLap: null, autoClosed: false };

export function reduceManual(s: SelState, slug: string): SelState {
  return { slug, locked: true, manual: true, startLap: s.startLap, autoClosed: s.autoClosed };
}

export function reduceAuto(s: SelState, detected: string | null, currentLap: number, moving: boolean): SelState {
  if (s.locked || s.manual) return s;                 // never override a lock/manual pick
  if (s.autoClosed) return s;                          // window permanently closed; never reopen
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
  if (detected) return { slug: detected, locked: true, manual: false, startLap, autoClosed: false };
  return { ...s, startLap };
}
