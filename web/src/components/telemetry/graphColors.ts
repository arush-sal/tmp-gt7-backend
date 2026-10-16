import type { Channel } from "../../lib/historyStore";

// Per-channel stroke colours (literal hex, theme-invariant — matches the wireframe colour key).
export const CHANNEL_COLOR: Record<Channel, string> = {
  throttle: "#1e6fd9",    // blue
  brake: "#1e6fd9",    // blue
  clutch: "#1e6fd9",    // blue
  rpm: "#1e6fd9",    // blue
  gear: "#1e6fd9",    // blue
  speed: "#1e6fd9",    // blue
  boost: "#1e6fd9",    // blue
  delta: "#f5d020",    // amber
  tyreFL: "#e10600",   // red
  tyreFR: "#f5d020",   // amber
  tyreRL: "#1e6fd9",   // blue
  tyreRR: "#2ed16a",   // green
  fuel: "#2ed16a",     // green
};

// Median + reference: solid, distinct from every channel colour and from each other.
export const MEDIAN_COLOR = "#ff8c00";    // orange
export const REFERENCE_COLOR = "#F433FF"; // violet
