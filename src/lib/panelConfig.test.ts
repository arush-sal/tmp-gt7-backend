// @vitest-environment jsdom
import { loadCardConfig, saveCardConfig, defaultCardConfig } from "./panelConfig";

const KEY = "gt7:test-cards";
beforeEach(() => localStorage.clear());

describe("panelConfig", () => {
  it("round-trips a saved config", () => {
    const cfg = { order: ["speed", "gear"], cards: { speed: { collapsed: true, hidden: ["median"] }, gear: { collapsed: false, hidden: [] } } };
    saveCardConfig(KEY, cfg);
    expect(loadCardConfig(KEY, defaultCardConfig())).toEqual(cfg);
  });

  it("returns the fallback when nothing is stored", () => {
    const fb = defaultCardConfig();
    expect(loadCardConfig(KEY, fb)).toEqual(fb);
  });

  it("returns the fallback on malformed JSON", () => {
    localStorage.setItem(KEY, "{not json");
    const fb = defaultCardConfig();
    expect(loadCardConfig(KEY, fb)).toEqual(fb);
  });

  it("drops order keys not in the catalog, and their card entries", () => {
    saveCardConfig(KEY, { order: ["speed", "ghostcard"], cards: { speed: { collapsed: false, hidden: [] }, ghostcard: { collapsed: true, hidden: ["x"] } } });
    const out = loadCardConfig(KEY, defaultCardConfig());
    expect(out.order).toEqual(["speed"]);
    expect(out.cards.ghostcard).toBeUndefined();
    expect(out.cards.speed).toEqual({ collapsed: false, hidden: [] });
  });

  it("rejects a top-level array and uses the fallback", () => {
    localStorage.setItem(KEY, JSON.stringify([{ collapsed: false, hidden: [] }]));
    const fb = defaultCardConfig();
    expect(loadCardConfig(KEY, fb)).toEqual(fb);
  });

  it("rejects a structurally invalid config (bad card entry) and uses the fallback", () => {
    localStorage.setItem(KEY, JSON.stringify({ order: ["speed"], cards: { speed: { collapsed: "yes", hidden: 3 } } }));
    const fb = defaultCardConfig();
    expect(loadCardConfig(KEY, fb)).toEqual(fb);
  });

  it("defaultCardConfig orders the default set with empty per-card state", () => {
    const d = defaultCardConfig();
    expect(d.order).toEqual(["throttle","brake","speed","gear","tyre","fuel"]);
    expect(d.cards.speed).toEqual({ collapsed: false, hidden: [] });
  });
});
