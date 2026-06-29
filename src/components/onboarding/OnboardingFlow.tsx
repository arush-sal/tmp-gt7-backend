import { useEffect, useState } from "react";
import type { ChangeEvent } from "react";
import {
  DEFAULT_PREFS, deltaForTolerance, toleranceFor,
} from "./types";
import type { DeltaTolerance, OnboardingPrefs, SkillLevel } from "./types";

interface Props {
  onComplete: (prefs: OnboardingPrefs) => void;
}

const SKILLS: { id: SkillLevel; name: string; tag: string; desc: string }[] = [
  { id: "noob", name: "Noob", tag: "gentle",
    desc: "New to sim racing — slow, step-by-step cues with plenty of encouragement." },
  { id: "amateur", name: "Amateur", tag: "balanced",
    desc: "Comfortable on track — focused tips on your biggest time loss each lap." },
  { id: "pro", name: "Pro", tag: "data-first",
    desc: "Fast & consistent — terse, numbers-only callouts, no hand-holding." },
];

const STEP_LABELS = ["Step 1 of 3 · Your name", "Step 2 of 3 · Skill", "Step 3 of 3 · Consistency"];

/** Shared card chrome: brand line + progress dots. */
function Shell({ step, children }: { step: number; children: React.ReactNode }) {
  return (
    <div className="ob-screen">
      <div className="ob-card">
        <div className="ob-top">
          <span className="ob-brand">GT7 RACE Engineer · SETUP</span>
          <div className="ob-dots">
            {[0, 1, 2].map(i => (
              <span key={i} className={`ob-dot ${i === step ? "ob-dot--on" : ""}`} />
            ))}
          </div>
        </div>
        <span className="ob-step">{STEP_LABELS[step]}</span>
        {children}
      </div>
    </div>
  );
}

export default function OnboardingFlow({ onComplete }: Props) {
  const [step, setStep] = useState(0);
  const [prefs, setPrefs] = useState<OnboardingPrefs>(DEFAULT_PREFS);

  const next = () => setStep(s => Math.min(2, s + 1));
  const back = () => setStep(s => Math.max(0, s - 1));

  const onName = (e: ChangeEvent<HTMLInputElement>) =>
    setPrefs(p => ({ ...p, engineerName: e.target.value }));

  const onSkill = (skill: SkillLevel) =>
    setPrefs(p => ({ ...p, skill }));

  const onSlider = (e: ChangeEvent<HTMLInputElement>) => {
    const deltaSeconds = Number(e.target.value);
    setPrefs(p => ({ ...p, deltaSeconds, tolerance: toleranceFor(deltaSeconds) }));
  };

  const onTolerance = (tolerance: DeltaTolerance) =>
    setPrefs(p => ({ ...p, tolerance, deltaSeconds: deltaForTolerance(tolerance) }));

  // Keyboard: Enter advances the step (Skip when a button is focused so its
  // native activation isn't double-fired); on step 3, arrows nudge the slider
  // globally (skip when the range itself is focused — native stepping handles it).
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const active = document.activeElement;

      if (step === 2 && ["ArrowLeft", "ArrowRight", "ArrowUp", "ArrowDown"].includes(e.key)) {
        const onRange = active instanceof HTMLInputElement && active.type === "range";
        if (onRange) return;
        e.preventDefault();
        const dir = e.key === "ArrowRight" || e.key === "ArrowUp" ? 0.05 : -0.05;
        setPrefs(p => {
          const v = Math.min(1.5, Math.max(0, Number((p.deltaSeconds + dir).toFixed(2))));
          return { ...p, deltaSeconds: v, tolerance: toleranceFor(v) };
        });
        return;
      }

      if (e.key !== "Enter") return;
      if (active instanceof HTMLButtonElement) return;
      e.preventDefault();
      if (step < 2) setStep(s => s + 1);
      else onComplete(prefs);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [step, prefs, onComplete]);

  // ── Step 1: name ──────────────────────────────────────────────────────────
  if (step === 0) {
    return (
      <Shell step={0}>
        <h1 className="ob-title">What would you like to call me?</h1>
        <p className="ob-sub">
          Used for an audio conversation cue during your laps — you can ask me
          any details, and I'll coach you to your purple laps.
        </p>

        <label className="ob-field-label">Race engineer name · for voice</label>
        <input
          className="ob-input"
          placeholder="e.g. Gianpiero"
          value={prefs.engineerName}
          onChange={onName}
          autoFocus
        />

        <div className="ob-preview">
          <span className="ob-preview__label">Voice readout preview</span>
          <span className="ob-preview__quote">
            "Brake 12m later <b>on T3</b> — carry more speed to the apex."
          </span>
        </div>

        <div className="ob-foot">
          <span className="ob-foot__note">You can change this later.</span>
          <span className="ob-foot__spacer" />
          <button className="btn btn--primary" onClick={next}>Next →</button>
        </div>
      </Shell>
    );
  }

  // ── Step 2: skill level ─────────────────────────────────────────────────────
  if (step === 1) {
    return (
      <Shell step={1}>
        <h1 className="ob-title">How do you drive?</h1>
        <p className="ob-sub">Sets how blunt — and how frequent — my callouts get.</p>

        <div className="ob-options">
          {SKILLS.map(s => {
            const sel = prefs.skill === s.id;
            return (
              <button
                key={s.id}
                className={`ob-opt ${sel ? "ob-opt--sel" : ""}`}
                onClick={() => onSkill(s.id)}
              >
                <div className="ob-opt__head">
                  <span className="ob-opt__name">{s.name}</span>
                  <span className={`ob-opt__tag ${sel ? "ob-opt__tag--sel" : ""}`}>
                    {sel ? "selected" : s.tag}
                  </span>
                </div>
                <span className="ob-opt__desc">{s.desc}</span>
              </button>
            );
          })}
        </div>

        <div className="ob-foot">
          <button className="btn" onClick={back}>← Back</button>
          <span className="ob-foot__spacer" />
          <button className="btn btn--primary" onClick={next}>Next →</button>
        </div>
      </Shell>
    );
  }

  // ── Step 3: lap-delta tolerance ─────────────────────────────────────────────
  const pct = (prefs.deltaSeconds / 1.5) * 100;
  const tolerances: DeltaTolerance[] = ["tight", "balanced", "loose"];
  return (
    <Shell step={2}>
      <h1 className="ob-title">Allowed delta between laps</h1>
      <p className="ob-sub">How much lap-time swing before I flag you as inconsistent.</p>

      <div className="ob-delta-val">±{prefs.deltaSeconds.toFixed(2)}s</div>

      <div className="ob-slider-row">
        <span className="ob-slider-end">0.0s</span>
        <input
          className="ob-range"
          type="range"
          min={0}
          max={1.5}
          step={0.05}
          value={prefs.deltaSeconds}
          onChange={onSlider}
          style={{
            background: `linear-gradient(90deg, var(--green) 0 ${pct}%, var(--surface-3) ${pct}% 100%)`,
          }}
        />
        <span className="ob-slider-end">1.5s</span>
      </div>

      <div className="seg">
        {tolerances.map(t => (
          <button
            key={t}
            className={`seg-btn ${prefs.tolerance === t ? "seg-btn--on" : ""}`}
            onClick={() => onTolerance(t)}
          >
            {t}
          </button>
        ))}
      </div>

      <div className="ob-info">
        Laps within <b>±{prefs.deltaSeconds.toFixed(2)}s</b> count as consistent.
        Bigger swings trigger a "find your rhythm" note.
      </div>

      <div className="ob-foot">
        <button className="ob-link" onClick={() => onComplete(prefs)}>Skip for now</button>
        <span className="ob-foot__spacer" />
        <button className="btn" onClick={back}>← Back</button>
        <button className="btn btn--primary" onClick={() => onComplete(prefs)}>
          Start coaching →
        </button>
      </div>
    </Shell>
  );
}
