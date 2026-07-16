import { useEffect, useState } from "react";
import type { RkbRecord } from "../lib/rkbGeometry";

const cache = new Map<string, RkbRecord>();

export function useTrack(slug: string | null): { record: RkbRecord | null; loading: boolean; error: boolean } {
  const [state, setState] = useState<{ record: RkbRecord | null; loading: boolean; error: boolean }>(
    { record: slug ? cache.get(slug) ?? null : null, loading: false, error: false });

  useEffect(() => {
    if (!slug) { setState({ record: null, loading: false, error: false }); return; }
    const cached = cache.get(slug);
    if (cached) { setState({ record: cached, loading: false, error: false }); return; }
    const ctrl = new AbortController();
    let active = true;
    setState({ record: null, loading: true, error: false });
    fetch(`/v1/tracks/${slug}`, { signal: ctrl.signal })
      .then(r => { if (!r.ok) throw new Error(String(r.status)); return r.json(); })
      .then((rec: RkbRecord) => {
        cache.set(slug, rec);
        if (!active) return;
        setState({ record: rec, loading: false, error: false });
      })
      .catch((e) => {
        if (e.name === "AbortError") return;
        if (!active) return;
        setState({ record: null, loading: false, error: true });
      });
    return () => { active = false; ctrl.abort(); };
  }, [slug]);

  return state;
}
