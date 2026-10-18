// @vitest-environment jsdom
import { render, screen } from "@testing-library/react";
import SessionPicker, { sessionLabels, sessionTitle } from "./SessionPicker";
import type { SessionSummaryDTO } from "../contract/session";

const mk = (o: Partial<SessionSummaryDTO>) => ({
  id: "s1", start_time: "2026-07-02T10:00:00Z", car_name: "Honda NSX Type R '02", best_lap: "91.521s",
  current_lap: 3, total_laps: 0, status: "finished", track_slug: "suzuka", ...o,
}) as unknown as SessionSummaryDTO;
const names = { suzuka: "Suzuka Circuit" };
const props = { trackNames: names, error: false, replay: false, onSelect: () => {}, onOpen: () => {} };

test("label: track · car · best, with live suffix when active", () => {
  const l = (s: SessionSummaryDTO) => sessionLabels([s], names).get(s.id);
  expect(l(mk({}))).toBe("Suzuka Circuit · Honda NSX Type R '02 · 1:31.521");
  expect(l(mk({ status: "active" }))).toBe("Suzuka Circuit · Honda NSX Type R '02 · 1:31.521 · live");
});

test("label omits missing track and best lap; unknown slug falls back to the slug", () => {
  const l = (s: SessionSummaryDTO) => sessionLabels([s], names).get(s.id);
  expect(l(mk({ track_slug: null }))).toBe("Honda NSX Type R '02 · 1:31.521");
  expect(l(mk({ best_lap: null }))).toBe("Suzuka Circuit · Honda NSX Type R '02");
  expect(l(mk({ track_slug: "mystery-ring" }))).toBe("mystery-ring · Honda NSX Type R '02 · 1:31.521");
});

test("identical labels get (2), (3) by start time, oldest first", () => {
  const a = mk({ id: "a", start_time: "2026-07-02T10:00:00Z" });
  const b = mk({ id: "b", start_time: "2026-07-03T10:00:00Z" });
  const c = mk({ id: "c", start_time: "2026-07-01T10:00:00Z" });
  const m = sessionLabels([b, a, c], names);
  expect(m.get("c")).toBe("Suzuka Circuit · Honda NSX Type R '02 · 1:31.521");
  expect(m.get("a")).toBe("Suzuka Circuit · Honda NSX Type R '02 · 1:31.521 (2)");
  expect(m.get("b")).toBe("Suzuka Circuit · Honda NSX Type R '02 · 1:31.521 (3)");
});

test("duplicate suffixes are stable when start times are equal (tie broken by id, not arrival order)", () => {
  const x = mk({ id: "x", start_time: "2026-07-02T10:00:00Z" });
  const y = mk({ id: "y", start_time: "2026-07-02T10:00:00Z" });
  for (const order of [[x, y], [y, x]]) {
    const m = sessionLabels(order, names);
    expect(m.get("x")).toBe("Suzuka Circuit · Honda NSX Type R '02 · 1:31.521");
    expect(m.get("y")).toBe("Suzuka Circuit · Honda NSX Type R '02 · 1:31.521 (2)");
  }
});

test("title carries date, track, car, best lap, laps and status", () => {
  const t = sessionTitle(mk({ total_laps: 5 }), names);
  expect(t).toContain(new Date("2026-07-02T10:00:00Z").toLocaleString());
  for (const part of ["Suzuka Circuit", "Honda NSX Type R '02", "1:31.521", "3/5 laps", "finished"]) expect(t).toContain(part);
});

test("options use short labels + title tooltips; select title is the selected session's details; Live first", () => {
  const s = mk({});
  const { rerender } = render(<SessionPicker {...props} sessions={[s]} selectedId={null} />);
  const select = screen.getByRole("combobox");
  expect(select.getAttribute("title")).toBe("Live");
  expect((select.querySelectorAll("option")[0]).textContent).toBe("Live");
  const opt = select.querySelectorAll("option")[1];
  expect(opt.textContent).toBe("Suzuka Circuit · Honda NSX Type R '02 · 1:31.521");
  expect(opt.getAttribute("title")).toBe(sessionTitle(s, names));
  rerender(<SessionPicker {...props} sessions={[s]} selectedId="s1" />);
  expect(screen.getByRole("combobox").getAttribute("title")).toBe(sessionTitle(s, names));
});
