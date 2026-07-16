import { useLayoutEffect, useState } from "react";
import AppBar from "./components/AppBar";
import Dashboard from "./components/Dashboard";
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

  return (
    <>
      <AppBar theme={theme} onToggleTheme={toggleTheme} />
      <div className="app">
        {phase.kind === "onboarding" ? (
          <OnboardingFlow
            onComplete={prefs => {
              savePrefs(prefs);
              setPhase({ kind: "dashboard", prefs });
            }}
          />
        ) : (
          <Dashboard theme={theme} />
        )}
      </div>
    </>
  );
}
