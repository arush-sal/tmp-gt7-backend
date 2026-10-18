// @vitest-environment jsdom
import { render, screen, fireEvent } from "@testing-library/react";
import { TrackPicker } from "./TrackPicker";

test("TrackPicker lists items and reports a pick", () => {
  const onPick = vi.fn();
  render(<TrackPicker value={null} onPick={onPick}
    items={[{ slug: "suzuka", name: "Suzuka Circuit" }, { slug: "spa-francorchamps", name: "Spa" }]} />);
  fireEvent.change(screen.getByRole("combobox"), { target: { value: "spa-francorchamps" } });
  expect(onPick).toHaveBeenCalledWith("spa-francorchamps");
});

test("TrackPicker shows a provisional chip only while the auto-detected track is provisional", () => {
  const items = [{ slug: "suzuka", name: "Suzuka Circuit" }];
  const { rerender } = render(<TrackPicker value="suzuka" onPick={() => {}} items={items} provisional />);
  expect(screen.getByText("provisional")).toBeInTheDocument();
  rerender(<TrackPicker value="suzuka" onPick={() => {}} items={items} />);
  expect(screen.queryByText("provisional")).toBeNull();
});

test("TrackPicker select carries the full track name as its title", () => {
  render(<TrackPicker value="w" onPick={() => {}} items={[{ slug: "w", name: "Willow Springs International Raceway: Big Willow" }]} />);
  expect(screen.getByRole("combobox").getAttribute("title")).toBe("Willow Springs International Raceway: Big Willow");
});

const ITEMS = [{ slug: "suzuka", name: "Suzuka Circuit" }];

test("TrackPicker offers Auto-detect first when onAuto is given, and calls it", () => {
  const onAuto = vi.fn(), onPick = vi.fn();
  render(<TrackPicker value="suzuka" items={ITEMS} onPick={onPick} onAuto={onAuto} />);
  const opts = screen.getAllByRole("option");
  expect(opts[0]).toHaveTextContent("Auto-detect");
  fireEvent.change(screen.getByRole("combobox"), { target: { value: "" } });
  expect(onAuto).toHaveBeenCalledTimes(1);
  expect(onPick).not.toHaveBeenCalled();
});

test("TrackPicker has no Auto-detect option without onAuto", () => {
  render(<TrackPicker value="suzuka" items={ITEMS} onPick={() => {}} />);
  expect(screen.queryByText("Auto-detect")).toBeNull();
});

test("TrackPicker chips: confirmed by you / applies to next session / error", () => {
  const { rerender } = render(<TrackPicker value="suzuka" items={ITEMS} onPick={() => {}} source="user" />);
  expect(screen.getByText("confirmed by you")).toBeInTheDocument();
  rerender(<TrackPicker value="suzuka" items={ITEMS} onPick={() => {}} pending />);
  expect(screen.getByText("applies to next session")).toBeInTheDocument();
  expect(screen.queryByText("confirmed by you")).toBeNull();
  rerender(<TrackPicker value="suzuka" items={ITEMS} onPick={() => {}} error="Unknown track" />);
  expect(screen.getByRole("alert")).toHaveTextContent("Unknown track");
});
