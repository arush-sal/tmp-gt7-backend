// jest-dom matchers (toBeInTheDocument, toBeDisabled, toHaveAttribute, ...) used by
// component tests (e.g. GraphLegend.test.tsx).
import "@testing-library/jest-dom/vitest";

// Test-only polyfills for jsdom, which doesn't implement everything the browser does.
// Guarded so this file is also safe to load under the default 'node' environment
// (where `window` doesn't exist at all).
//
// matchMedia: uPlot's module-level init (setPxRatio) queries a devicePixelRatio media
// query on import, unconditionally - even in tests that never construct a chart
// (jsdom has no canvas, so TelemetryGraph itself is never unit-tested; this stub just
// lets *other* component tests that happen to import something which imports uplot
// load without crashing).
if (typeof window !== "undefined" && !window.matchMedia) {
  window.matchMedia = (query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  }) as unknown as MediaQueryList;
}

// ResizeObserver: jsdom doesn't implement it either. TelemetryGraph observes its
// container to size the uPlot canvas; component tests that mount it (without exercising
// canvas rendering, which jsdom can't do anyway) just need the constructor to exist and
// not throw. No-op is fine - it never fires, so TelemetryGraph never constructs a real
// uPlot instance in these tests (that path is verified visually, see task-234 report).
if (typeof window !== "undefined" && !window.ResizeObserver) {
  window.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  } as unknown as typeof ResizeObserver;
}
