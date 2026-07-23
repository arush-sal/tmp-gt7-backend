import { useEffect, useMemo, useState } from "react";
import type { Corner, CornerAnalysis } from "../contract/corner";
import { useSession } from "../hooks/useSession";
import { useHistory } from "../hooks/useHistory";
import { useLaps } from "../hooks/useLaps";
import { useLapTrace } from "../hooks/useLapTrace";
import { useTrack } from "../hooks/useTrack";
import { useSectorTimes } from "../hooks/useSectorTimes";
import { estimateLapsLeft } from "../lib/fuelEstimate";
import { subscribeLive, type LiveSource } from "../lib/liveSource";
import { gateAfterRace } from "../lib/gateAfterRace";
import { frameToDisplay } from "../lib/liveView";
import { lapDtoToSectorTimes } from "../lib/lapView";
import { resetLapCache } from "../lib/lapCache";
import { toDrawGeometry } from "../lib/rkbGeometry";
import { insideFraction, isMismatch } from "../lib/trackMismatch";
import { initialSel, reduceAuto, reduceManual } from "../lib/trackSelection";
import { clampIndex } from "../lib/trackScrub";
import { loadVersioned, saveVersioned } from "../lib/persistState";
import SessionHeader from "./SessionHeader";
import { TrackPicker, type TrackListItem } from "./track/TrackPicker";
import TrackTraceCanvas from "./track/TrackTraceCanvas";
import { ConfidenceBadges } from "./track/ConfidenceBadges";
import TelemetryPanel from "./telemetry/TelemetryPanel";
import FloatingPanel from "./telemetry/FloatingPanel";
import CornerDetailPanel from "./CornerDetailPanel";
import RightPanel from "./RightPanel";

export default function Dashboard({ source = subscribeLive, theme = "light", onLiveChange }: {
  source?: LiveSource; theme?: "light" | "dark"; onLiveChange?: (live: boolean) => void;
}) {
  // Gate the stream once, share it with both subscriptions: after a race finishes, one latch
  // stops cooldown frames reaching history, sectors, view, and track alike (see gateAfterRace).
  const gated = useMemo(() => gateAfterRace(source), [source]);
  const { snapshot, error, view, live, newRaceSeq, raceKey } = useSession(undefined, gated);
  const sessionId = raceKey;
  // Independent live subscription (per the plan: fan-out to a single subscription is a
  // later pass) — feeds the graph buffer only, reuses the same gated `source` as
  // useSession so tests (and a future single-subscription refactor) see one seam. On each lap
  // rollover the store hands the finished lap to lapCache keyed by `sessionId`.
  const history = useHistory(gated, sessionId);
  // Single writer into the history store: gate append by `live` (paused/idle/race-over
  // stops growth) and wipe on a genuine new race. The sector timer resets itself on
  // session_id change via its own sessionKey, so it needs no explicit reset here.
  useEffect(() => { history.setLive(live); }, [history, live]);
  useEffect(() => { if (newRaceSeq > 0) { history.reset(); resetLapCache(); } }, [history, newRaceSeq]);
  useEffect(() => { onLiveChange?.(live); }, [onLiveChange, live]);

  // Fuel card: used/lap from the most recently completed lap; laps left projects the
  // current tank against that burn rate. Both null when data is insufficient (e.g. no
  // completed lap yet, or the store detected a refuel anomaly).
  const usedPerLap = history.lastLapFuelUsedPct();
  const currentFuelPct = snapshot?.derived?.fuel_pct ?? null;
  const lapsLeft = estimateLapsLeft(currentFuelPct, usedPerLap);

  // Corner/coaching data is not emitted by the backend yet (coaching is a
  // `status:"pending"` passthrough). Until it ships, there are no corners to select
  // and no detail to show — the panels render their pending/empty states. The
  // selection plumbing is kept intact so wiring real data back in is a one-liner.
  const corners: Corner[] = [];
  const [selectedCornerId, setSelectedCornerId] = useState<number | null>(null);
  const selectedCornerDetail: CornerAnalysis | null = null;
  const handleCornerSelect = (id: number | null) => setSelectedCornerId(id);

  // Telemetry panel is docked on the left of the track area. Collapsing hides it entirely so
  // the track canvas fills the freed width; the open/closed choice persists across refreshes.
  const [telemetryOpen, setTelemetryOpen] = useState(() => {
    try { return localStorage.getItem("gt7:telemetry-open") !== "0"; } catch { return true; }
  });
  useEffect(() => {
    try { localStorage.setItem("gt7:telemetry-open", telemetryOpen ? "1" : "0"); } catch { /* no storage */ }
  }, [telemetryOpen]);

  // Track list for the picker — fetched once; failure just leaves the list empty.
  const [trackList, setTrackList] = useState<TrackListItem[]>([]);
  useEffect(() => {
    let active = true;
    const ctrl = new AbortController();
    fetch("/v1/tracks", { signal: ctrl.signal })
      .then(r => { if (!r.ok) throw new Error(String(r.status)); return r.json(); })
      .then((items: TrackListItem[]) => { if (active) setTrackList(items); })
      .catch((e) => { if (e.name === "AbortError") return; if (active) setTrackList([]); });
    return () => { active = false; ctrl.abort(); };
  }, []);

  // Track selection: auto-detect within the first observed lap, else stay pickable manually.
  const [sel, setSel] = useState(() => {
    const saved = loadVersioned<string>("gt7:track");
    return saved ? { ...initialSel, slug: saved } : initialSel;
  });
  useEffect(() => { if (sel.slug) saveVersioned("gt7:track", sel.slug); }, [sel.slug]);
  useEffect(() => { if (newRaceSeq > 0) setSel(initialSel); }, [newRaceSeq]);
  useEffect(() => {
    setSel(s => reduceAuto(
      s,
      snapshot?.enrichment?.track ?? null,
      snapshot?.frame?.current_lap ?? 0,
      (snapshot?.frame?.car_speed ?? 0) > 1,
    ));
  }, [snapshot]);

  const track = useTrack(sel.slug);
  const geometry = useMemo(() => (track.record ? toDrawGeometry(track.record) : null), [track.record]);
  const sectorTimes = useSectorTimes(snapshot, geometry, sel.slug, live);

  // Best-candidate shortlist for the header picker: cap to 3 so drivers aren't scrolling
  // through all 121 tracks. Interim heuristic only -- true detection-ranked candidates
  // need a backend `track_candidates` field (follow-up); until then we approximate with
  // the auto-detected slug (if any) plus tracks whose coordinates are known-reliable.
  const candidates = useMemo(() => {
    const autoSlug = sel.slug ?? snapshot?.enrichment?.track ?? null;
    const out: TrackListItem[] = [];
    const seen = new Set<string>();
    const add = (item: TrackListItem | undefined) => {
      if (item && !seen.has(item.slug)) { seen.add(item.slug); out.push(item); }
    };
    if (autoSlug) add(trackList.find(t => t.slug === autoSlug));
    for (const t of trackList) {
      if (out.length >= 3) break;
      if (t.coordinate_usability === "generated-reliable") add(t);
    }
    return out.slice(0, 3);
  }, [trackList, sel.slug, snapshot?.enrichment?.track]);

  // Lap selection: which lap's trace is drawn on the canvas. A numeric `selectedLap` is a
  // lap_number (1-based). Auto-follows: "current" (live view) while live, the latest completed
  // lap once live ends -- until the user picks explicitly.
  const [selectedLap, setSelectedLap] = useState<"all" | "current" | number>("current");
  const [userPickedLap, setUserPickedLap] = useState(false);
  const completedLapCount = history.completedLapCount();
  // Authoritative lap list from the backend (survives a page refresh — the live counter above
  // resets to 0 on reload). Revalidated whenever a lap finishes live (completedLapCount trigger),
  // and unioned with the live counter so a just-finished lap appears instantly.
  const laps = useLaps(sessionId, completedLapCount);
  // Only real racing laps are selectable: the backend also serves the formation lap (0) and the
  // cooldown lap (> total_laps) -- exclude both so the dropdown never shows "Lap 0" or a "Lap 11"
  // for a 10-lap race. total_laps <= 0 (time trial) => no upper cap.
  const totalLaps = snapshot?.frame?.total_laps ?? 0;
  const backendMaxLap = laps.laps.reduce(
    (m, l) => (l.lap_number >= 1 && (totalLaps <= 0 || l.lap_number <= totalLaps) && l.lap_number > m ? l.lap_number : m),
    0);
  const latestLap = Math.max(backendMaxLap, completedLapCount);
  useEffect(() => {
    if (userPickedLap) return;
    setSelectedLap(live ? "current" : latestLap > 0 ? latestLap : "current");
  }, [live, latestLap, userPickedLap]);
  // New race: drop back to auto-follow AND clear a stale numeric selectedLap (completedLapCount
  // resets to 0 too, but not necessarily in the same tick -- the <select> must never hold a
  // value with no matching option).
  useEffect(() => { if (newRaceSeq > 0) { setUserPickedLap(false); setSelectedLap("current"); } }, [newRaceSeq]);

  // A completed lap's full trace comes from the backend, cache-first (the live lapCache serves
  // in-session laps with no fetch); `laps` (above) supplies its sector splits.
  const lapTrace = useLapTrace(sessionId, typeof selectedLap === "number" ? selectedLap : null);
  const lapView = lapTrace.view;
  // Analysis mode: a completed lap is selected -> a scrub cursor can be placed/stepped
  // along that lap's trace, and the telemetry graphs switch to showing that lap's data.
  const analysisLap = lapView?.points ?? null;
  const [scrubIndex, setScrubIndex] = useState<number | null>(null);
  // Header, graphs, and canvas cursor must agree on the default scrub position: a specific
  // lap starts scrubbed to its LAST index (lap-end), not null/0. Re-keys off lapView so it
  // settles once the (possibly async) trace resolves.
  useEffect(() => {
    setScrubIndex(typeof selectedLap === "number" ? (lapView?.length ?? 1) - 1 : null);
  }, [selectedLap, lapView]);
  const frame = typeof selectedLap === "number" && scrubIndex !== null
    ? lapView?.frameAt(scrubIndex) ?? null
    : null;
  const displayView = view && frame ? { ...view, ...frameToDisplay(frame) } : view;
  // Sector splits for a selected lap come straight from its LapDTO (backend-computed); live
  // sector timing only applies to the current lap.
  const selectedLapDto = typeof selectedLap === "number"
    ? laps.laps.find(l => l.lap_number === selectedLap)
    : undefined;
  const shownSectors = typeof selectedLap === "number"
    ? (selectedLapDto ? lapDtoToSectorTimes(selectedLapDto) : null)
    : sectorTimes;
  useEffect(() => {
    if (!analysisLap) return;
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return;
      e.preventDefault();
      const dir = e.key === "ArrowRight" ? 1 : -1;
      setScrubIndex(prev => clampIndex((prev ?? 0) + dir, analysisLap.length));
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [analysisLap]);

  // Mismatch check is a dev nudge, not per-frame work — throttle to 1/s.
  const [mismatch, setMismatch] = useState(false);
  useEffect(() => {
    if (!geometry) { setMismatch(false); return; }
    const id = setInterval(() => {
      setMismatch(isMismatch(insideFraction(history.trace().current, geometry)));
    }, 1000);
    return () => clearInterval(id);
  }, [geometry, history]);

  // The track name itself is redundant here -- it's already shown in the header picker --
  // so the canvas footer only carries actionable status text (error / still-detecting /
  // no-match), alongside the confidence badges.
  const lapNote =
    typeof selectedLap === "number" && lapTrace.loading ? `loading lap ${selectedLap}…`
      : typeof selectedLap === "number" && lapTrace.error ? `couldn't load lap ${selectedLap}`
        : null;
  const statusMessage = error
    ? `stream error: ${error.message}`
    : lapNote
      ? lapNote
      : sel.slug
        ? null
        : !sel.autoClosed
          ? "detecting track..."
          : "no track detected";

  return (
    <>
      {(!snapshot || error || snapshot.status === "idle") && (
        <div className="conn-banner">
          {error
            ? `stream error: ${error.message}`
            : !snapshot
              ? "connecting — waiting for telemetry…"
              : "waiting for telemetry — start a session in GT7"}
        </div>
      )}

      <SessionHeader
        view={displayView}
        selectedCorner={selectedCornerDetail}
        trackPicker={
          // No selected/detected track (e.g. right after a refresh, before auto-detect
          // re-locks) -> render nothing rather than an empty "select track" dropdown.
          sel.slug ? (
            <TrackPicker
              value={sel.slug}
              items={candidates}
              onPick={slug => setSel(s => reduceManual(s, slug))}
            />
          ) : undefined
        }
        lapSelector={
          <select
            aria-label="Lap"
            className="header-select"
            value={String(selectedLap)}
            onChange={e => {
              const v = e.target.value;
              setSelectedLap(v === "all" || v === "current" ? v : Number(v));
              setUserPickedLap(true);
            }}
          >
            <option value="all">All laps</option>
            <option value="current">Current lap</option>
            {Array.from({ length: latestLap }, (_, i) => latestLap - i).map(n => (
              <option key={n} value={n}>Lap {n}</option>
            ))}
          </select>
        }
      />

      <div className="dashboard-body">
        <div className="center-column">
          {telemetryOpen ? (
            <aside className="telemetry-dock">
              <button
                type="button"
                className="telemetry-dock__toggle chip"
                onClick={() => setTelemetryOpen(false)}
                aria-label="hide telemetry"
                title="hide telemetry"
              >
                ◀
              </button>
              <TelemetryPanel
                store={history}
                analysisLap={lapView}
                analysisScrubIndex={scrubIndex}
              />
            </aside>
          ) : (
            <button
              type="button"
              className="telemetry-reopen chip"
              onClick={() => setTelemetryOpen(true)}
              aria-label="show telemetry"
              title="show telemetry"
            >
              graphs ▶
            </button>
          )}
          <div className="track-panel">
            <TrackTraceCanvas
              store={history}
              geometry={geometry}
              theme={theme}
              selectedLap={selectedLap}
              analysisLap={analysisLap}
              scrubIndex={scrubIndex}
              onScrub={setScrubIndex}
              footer={
                <>
                  {statusMessage && <span className="track-status-text">{statusMessage}</span>}
                  <ConfidenceBadges
                    coordinate={track.record?.confidence?.coordinate_usability}
                    boundary={track.record?.confidence?.physical_boundary_fidelity}
                    trackConfidence={snapshot?.enrichment?.track_confidence}
                    mismatch={mismatch}
                  />
                </>
              }
            />
          </div>
          {selectedCornerDetail !== null && (
            <FloatingPanel
              key="corner"
              storageKey="gt7:corner-geom"
              defaultGeom={{ x: 400, y: 12, width: 320, height: 360 }}
              dragHandleClassName="corner-detail__header"
            >
              <CornerDetailPanel corner={selectedCornerDetail} onClear={() => setSelectedCornerId(null)} />
            </FloatingPanel>
          )}
        </div>

        <div className="right-panel-wrap">
          <RightPanel
            view={displayView}
            corners={corners}
            selectedCornerId={selectedCornerId}
            onCornerSelect={handleCornerSelect}
            selectedCornerDetail={selectedCornerDetail}
            usedPerLap={usedPerLap}
            lapsLeft={lapsLeft}
            sectorTimes={shownSectors}
          />
        </div>
      </div>
    </>
  );
}
