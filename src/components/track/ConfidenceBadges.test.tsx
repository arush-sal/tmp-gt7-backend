// @vitest-environment jsdom
import { render, screen } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import { ConfidenceBadges } from "./ConfidenceBadges";

afterEach(() => vi.unstubAllEnvs());

test("renders all tier badges in dev", () => {
  render(<ConfidenceBadges coordinate="generated-coarse" boundary="derived"
    trackConfidence="confident" mismatch={true} />);
  expect(screen.getByText(/generated-coarse/)).toBeInTheDocument();
  expect(screen.getByText(/derived/)).toBeInTheDocument();
  expect(screen.getByText(/confident/)).toBeInTheDocument();
  expect(screen.getByText(/mismatch/i)).toBeInTheDocument();
});

test("renders nothing in production (dev-gate, REMOVE-BEFORE-V1)", () => {
  vi.stubEnv("DEV", false);
  const { container } = render(
    <ConfidenceBadges coordinate="generated-coarse" boundary="derived"
      trackConfidence="confident" mismatch={true} />);
  expect(container).toBeEmptyDOMElement();
});
