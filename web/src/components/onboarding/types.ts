// Onboarding preference shape — collected across the 3 setup steps.
// Literal unions (not raw strings/numbers) so invalid values can't compile.

export type SkillLevel = "noob" | "amateur" | "pro";
export type DeltaTolerance = "tight" | "balanced" | "loose";

export interface OnboardingPrefs {
  /** Race-engineer voice name. Empty string is allowed (user can skip). */
  engineerName: string;
  skill: SkillLevel;
  /** Raw slider value in seconds (0.0–1.5). */
  deltaSeconds: number;
  /** Band derived from deltaSeconds, surfaced as the tight/balanced/loose toggle. */
  tolerance: DeltaTolerance;
}

export const DEFAULT_PREFS: OnboardingPrefs = {
  engineerName: "",
  skill: "amateur",
  deltaSeconds: 0.4,
  tolerance: "balanced",
};

/** Map a raw slider value to its tolerance band. */
export function toleranceFor(deltaSeconds: number): DeltaTolerance {
  if (deltaSeconds <= 0.25) return "tight";
  if (deltaSeconds <= 0.7) return "balanced";
  return "loose";
}

/** Representative slider value when a tolerance band button is clicked. */
export function deltaForTolerance(t: DeltaTolerance): number {
  switch (t) {
    case "tight": return 0.2;
    case "balanced": return 0.4;
    case "loose": return 1.0;
  }
}
