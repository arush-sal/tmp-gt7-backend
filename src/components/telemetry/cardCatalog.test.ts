import { CARD_CATALOG, DEFAULT_CARD_KEYS, getCard } from "./cardCatalog";

const KEYS = new Set(["throttle","brake","clutch","rpm","gear","speed","boost","delta","tyreFL","tyreFR","tyreRL","tyreRR","fuel"]);

describe("cardCatalog", () => {
  it("has unique keys", () => {
    const keys = CARD_CATALOG.map(c => c.key);
    expect(new Set(keys).size).toBe(keys.length);
  });

  it("every channel used exists in the historyStore Channel union", () => {
    for (const c of CARD_CATALOG) for (const ch of c.channels) expect(KEYS.has(ch)).toBe(true);
  });

  it("steering is a needs-backend entry with no channels", () => {
    const steering = getCard("steering");
    expect(steering?.availability).toBe("needs-backend");
    expect(steering?.channels).toEqual([]);
  });

  it("default set is throttle, brake, speed, rpm, gear, tyre in order", () => {
    expect(DEFAULT_CARD_KEYS).toEqual(["throttle","brake","speed","rpm","gear","tyre"]);
  });

  it("default keys all resolve to available catalog entries", () => {
    for (const k of DEFAULT_CARD_KEYS) {
      const c = getCard(k);
      expect(c).toBeDefined();
      expect(c!.availability).toBe("available");
    }
  });

  it("the tyre card is multi-series over four corners", () => {
    const tyre = getCard("tyre");
    expect(tyre?.kind).toBe("multi");
    expect(tyre?.channels).toEqual(["tyreFL","tyreFR","tyreRL","tyreRR"]);
  });
});
