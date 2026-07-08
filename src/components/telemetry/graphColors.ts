import type { Channel } from "../../lib/historyStore";

// Per-channel stroke colours (literal hex, theme-invariant — matches the wireframe colour key).
export const CHANNEL_COLOR: Record<Channel, string> = {
  throttle: "#2ed16a", // green
  brake: "#e10600",    // red
  clutch: "#f5d020",   // amber
  rpm: "#1e6fd9",      // blue
  gear: "#b14fff",     // purple
  speed: "#1e6fd9",    // blue
  boost: "#b14fff",    // purple
  delta: "#f5d020",    // amber
  tyreFL: "#e10600",   // red
  tyreFR: "#f5d020",   // amber
  tyreRL: "#1e6fd9",   // blue
  tyreRR: "#2ed16a",   // green
  fuel: "#2ed16a",     // green
};

// Median + reference: solid, distinct from every channel colour and from each other.
export const MEDIAN_COLOR = "#ff8c00";    // orange
export const REFERENCE_COLOR = "#00c2d1"; // cyan
