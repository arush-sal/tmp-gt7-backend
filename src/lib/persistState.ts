export const SCHEMA_VERSION = 1;

// Parse a versioned blob; returns the inner payload only if schemaVersion matches, else null.
export function loadVersioned<T>(key: string): T | null {
  try {
    const raw = localStorage.getItem(key);
    if (!raw) return null;
    const parsed = JSON.parse(raw) as { schemaVersion?: unknown; payload?: unknown } | null;
    if (!parsed || parsed.schemaVersion !== SCHEMA_VERSION) return null;
    return parsed.payload as T;
  } catch {
    return null;
  }
}

// Wrap payload as { schemaVersion, payload } and store; swallow QuotaExceeded / no-storage.
export function saveVersioned<T>(key: string, payload: T): void {
  try {
    localStorage.setItem(key, JSON.stringify({ schemaVersion: SCHEMA_VERSION, payload }));
  } catch {
    /* quota / no storage — best effort */
  }
}
