import type { Channel } from "../../lib/historyStore";

export type CardAvailability = "available" | "needs-backend";
export type CardKind = "single" | "multi";

export interface CardDef {
  key: string;              // stable id used in persistence
  title: string;
  unit: string;
  channels: Channel[];      // 1 for single, 4 for multi, 0 for needs-backend
  kind: CardKind;
  availability: CardAvailability;
}

export const CARD_CATALOG: CardDef[] = [
  { key: "throttle", title: "Throttle", unit: "%",    channels: ["throttle"], kind: "single", availability: "available" },
  { key: "brake",    title: "Brake",    unit: "%",    channels: ["brake"],    kind: "single", availability: "available" },
  { key: "speed",    title: "Speed",    unit: "km/h", channels: ["speed"],    kind: "single", availability: "available" },
  { key: "gear",     title: "Gear",     unit: "",     channels: ["gear"],     kind: "single", availability: "available" },
  { key: "rpm",      title: "RPM",      unit: "rpm",  channels: ["rpm"],      kind: "single", availability: "available" },
  { key: "boost",    title: "Boost",    unit: "",     channels: ["boost"],    kind: "single", availability: "available" },
  { key: "fuel",     title: "Fuel",     unit: "%",    channels: ["fuel"],     kind: "single", availability: "available" },
  { key: "tyre",     title: "Tyre Temp", unit: "°C",
    channels: ["tyreFL","tyreFR","tyreRL","tyreRR"], kind: "multi", availability: "available" },
  // Not decoded by the backend yet (raw GT7 'C' packet only). Shown greyed, non-addable.
  { key: "steering", title: "Steering", unit: "°", channels: [], kind: "single", availability: "needs-backend" },
];

export const DEFAULT_CARD_KEYS = ["throttle", "brake", "speed", "rpm", "gear", "tyre"];

const BY_KEY = new Map(CARD_CATALOG.map(c => [c.key, c]));
export function getCard(key: string): CardDef | undefined { return BY_KEY.get(key); }
