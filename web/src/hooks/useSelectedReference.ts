import { useEffect, useRef, useState } from "react";
import type { ReferenceDTO, StoredReferenceDTO } from "../contract/session";
import { fetchStoredReference } from "../lib/referenceApi";
import type { RefSource } from "../lib/referenceSource";

const REFRESH_DEBOUNCE_MS = 300;

export type SelectedReference = {
  reference: ReferenceDTO | null;  // the ONE reference every consumer reads
  effective: RefSource;            // what is actually in use (stored falls back to predicted)
  stored: StoredReferenceDTO | null;
  noStored: boolean;               // fetch resolved 404: no stored lap for this track
  error: boolean;                  // fetch failed (not 404): surfaced, never silently falling back
};

/**
 * useSelectedReference fetches the track's stored reference (refetched when `slug` or `pinVersion`
 * changes; no request for an empty slug) and picks between it and the session's predicted
 * reference. `refreshKey` (e.g. completed-lap count) also refetches, debounced so a lap rollover is
 * one request: a track may gain its first stored lap mid-session. A "stored" choice falls back to predicted when the track has no stored lap.
 */
export function useSelectedReference(
  slug: string | null, source: RefSource, sessionReference: ReferenceDTO | null, pinVersion = 0, refreshKey: number | string = 0,
): SelectedReference {
  const [state, setState] = useState<{ key: string; slug: string; stored: StoredReferenceDTO | null; error: boolean } | null>(null);
  const key = `${slug}:${pinVersion}`;
  // Slug/pin changes fetch immediately; a refreshKey-only change waits REFRESH_DEBOUNCE_MS and coalesces.
  const last = useRef(key);
  useEffect(() => {
    if (!slug) { setState(null); return; }
    const ctrl = new AbortController();
    let active = true;
    const run = () => fetchStoredReference(slug, undefined, ctrl.signal)
      .then(d => { if (active) setState({ key, slug, stored: d, error: false }); })
      .catch(e => { if (e.name !== "AbortError" && active) setState({ key, slug, stored: null, error: true }); });
    const immediate = last.current !== key || state === null;
    last.current = key;
    const timer = setTimeout(run, immediate ? 0 : REFRESH_DEBOUNCE_MS);
    return () => { active = false; clearTimeout(timer); ctrl.abort(); };
  }, [slug, pinVersion, refreshKey]); // eslint-disable-line react-hooks/exhaustive-deps
  // Ignore a result fetched for a previous slug so a stale reference never shows on the new track;
  // across a pin change (same slug) keep the old value until the refetch lands, to avoid a flicker.
  const stored = slug && state && state.slug === slug ? state.stored : null;
  const error = !!slug && state?.slug === slug && state.error;
  const useStored = source === "stored" && stored != null;
  // A failed load is not "no stored lap": show NO reference rather than quietly using the predicted one.
  const failed = source === "stored" && error;
  return {
    reference: failed ? null : useStored ? stored : sessionReference,
    effective: useStored || failed ? "stored" : "predicted",
    stored,
    error,
    noStored: !!slug && !error && state?.key === key && state.stored == null,
  };
}
