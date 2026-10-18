import { useLayoutEffect, useState } from "react";
import AppBar from "./components/AppBar";
import Dashboard from "./components/Dashboard";
import { useMode } from "./hooks/useMode";
import OnboardingFlow from "./components/onboarding/OnboardingFlow";
import type { OnboardingPrefs } from "./components/onboarding/types";
import "./index.css";

type Theme = "light" | "dark";

// Discriminated union: a dashboard phase always carries completed prefs, so
// "dashboard without setup" is unrepresentable (can't be constructed).
type AppPhase =
  | { kind: "onboarding" }
  | { kind: "dashboard"; prefs: OnboardingPrefs };

// Persist onboarding prefs client-side so a refresh skips setup. localStorage
// (not a cookie): it stays in the browser and isn't sent on every request.
const PREFS_KEY = "gt7:onboarding-prefs";
const THEME_KEY = "gt7:theme";

function loadPrefs(): OnboardingPrefs | null {
  try {
    const raw = localStorage.getItem(PREFS_KEY);
    if (!raw) return null;
    const p = JSON.parse(raw) as Partial<OnboardingPrefs>;
    // Light shape guard — corrupt/old data falls back to onboarding.
    if (
      typeof p.engineerName === "string" &&
      typeof p.skill === "string" &&
      typeof p.tolerance === "string" &&
      typeof p.deltaSeconds === "number"
    ) {
      return p as OnboardingPrefs;
    }
    return null;
  } catch {
    return null;
  }
}

function savePrefs(p: OnboardingPrefs): void {
  try {
    localStorage.setItem(PREFS_KEY, JSON.stringify(p));
  } catch {
    // storage disabled/full — non-fatal, just won't persist.
  }
}

export default function App() {
  const [theme, setTheme] = useState<Theme>(() => {
    try {
      const t = localStorage.getItem(THEME_KEY);
      return t === "dark" || t === "light" ? t : "light";
    } catch {
      return "light"; // storage disabled/blocked — non-fatal, fall back to light
    }
  });
  // Lazy initialiser runs once: resume the dashboard if prefs were saved.
  const [phase, setPhase] = useState<AppPhase>(() => {
    const saved = loadPrefs();
    return saved ? { kind: "dashboard", prefs: saved } : { kind: "onboarding" };
  });

  // useLayoutEffect applies the theme before the browser paints → no flash.
  useLayoutEffect(() => {
    document.documentElement.setAttribute("data-theme", theme);
    try { localStorage.setItem(THEME_KEY, theme); } catch { /* storage disabled */ }
  }, [theme]);

  const toggleTheme = () => setTheme(t => (t === "light" ? "dark" : "light"));
  const mode = useMode(); // null while /v1/mode loads: the dashboard waits so analysis never flashes live UI
  const [live, setLive] = useState(false);
  // The bar owns the slot element; Dashboard portals the record toggle into it.
  const [recordSlot, setRecordSlot] = useState<HTMLElement | null>(null);

  return (
    <>
      <AppBar theme={theme} onToggleTheme={toggleTheme} live={live} mode={mode ?? undefined} recordSlotRef={phase.kind === "dashboard" ? setRecordSlot : undefined} />
      <div className="app">
        {phase.kind === "onboarding" ? (
          <OnboardingFlow
            onComplete={prefs => {
              savePrefs(prefs);
              setPhase({ kind: "dashboard", prefs });
            }}
          />
        ) : mode && (
          <Dashboard recordSlot={recordSlot} theme={theme} onLiveChange={setLive} mode={mode} />
        )}
      </div>
    </>
  );
}
