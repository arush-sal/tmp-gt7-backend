import { getCard, DEFAULT_CARD_KEYS } from "../components/telemetry/cardCatalog";

export interface CardState { collapsed: boolean; hidden: string[]; }
export interface PanelConfig { order: string[]; cards: Record<string, CardState>; }

const isCardState = (v: unknown): v is CardState =>
  typeof v === "object" && v !== null && !Array.isArray(v) &&
  typeof (v as CardState).collapsed === "boolean" &&
  Array.isArray((v as CardState).hidden) &&
  (v as CardState).hidden.every(h => typeof h === "string");

const isPanelConfig = (v: unknown): v is PanelConfig => {
  if (typeof v !== "object" || v === null || Array.isArray(v)) return false;
  const c = v as PanelConfig;
  if (!Array.isArray(c.order) || !c.order.every(k => typeof k === "string")) return false;
  if (typeof c.cards !== "object" || c.cards === null || Array.isArray(c.cards)) return false;
  return Object.values(c.cards).every(isCardState);
};

/**
 * localStorage key for the telemetry panel layout. Bumped v2 -> v3 on 2026-10-18 so every
 * browser drops its saved layout ONCE and picks up the new defaults (throttle, brake, steering,
 * gear first; median overlay off). Old layouts are deliberately not migrated. Bump again only
 * when a default change must override saved layouts.
 */
export const TELEMETRY_CARDS_KEY = "gt7:telemetry-cards-v3";

/** Fresh per-card state: expanded, median overlay off (reference/current stay visible). */
export const newCardState = (): CardState => ({ collapsed: false, hidden: ["median"] });

export function defaultCardConfig(): PanelConfig {
  const cards: Record<string, CardState> = {};
  for (const k of DEFAULT_CARD_KEYS) cards[k] = newCardState();
  return { order: [...DEFAULT_CARD_KEYS], cards };
}

export function loadCardConfig(key: string, fallback: PanelConfig): PanelConfig {
  try {
    const raw = localStorage.getItem(key);
    if (!raw) return fallback;
    const parsed = JSON.parse(raw);
    if (!isPanelConfig(parsed)) return fallback;
    // Drop stale keys (catalog changed since save) from BOTH order and cards, so a later
    // save can't write stale entries back and a reintroduced key gets fresh defaults.
    const order = parsed.order.filter(k => getCard(k) !== undefined);
    const cards: Record<string, CardState> = {};
    for (const k of order) if (isCardState(parsed.cards[k])) cards[k] = parsed.cards[k];
    return { order, cards };
  } catch {
    return fallback;
  }
}

export function saveCardConfig(key: string, cfg: PanelConfig): void {
  try {
    localStorage.setItem(key, JSON.stringify(cfg));
  } catch {
    /* storage disabled/full — non-fatal */
  }
}
