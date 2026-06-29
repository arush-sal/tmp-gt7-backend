import type { DashboardSnapshot } from "../contract/dashboard";

/**
 * Validates raw JSON from the API and returns a typed DashboardSnapshot.
 * Throws an Error with a descriptive message if anything doesn't match.
 *
 * This is the boundary function — everything inside the app can trust
 * DashboardSnapshot because this function stood guard at the door.
 *
 * --- SESSION PROGRESS ---
 * Built together during tutoring. Completed sections are marked ✅.
 * Sections still to implement are marked TODO with Socratic hints.
 */
export function validate(raw: unknown): DashboardSnapshot {

  // ✅ Check that the top-level value is a non-null object.
  if (typeof raw !== "object" || raw === null) {
    throw new Error("snapshot: expected a non-null object");
  }
  const snap = raw as Record<string, unknown>;

  // =========================================================================
  // ✅ position
  // =========================================================================
  if (typeof snap.position !== "object" || snap.position === null) {
    throw new Error("snapshot.position: expected a non-null object");
  }
  const pos = snap.position as Record<string, unknown>;

  if (typeof pos.current !== "number") {
    throw new Error("snapshot.position.current: expected a number");
  }
  if (typeof pos.total !== "number") {
    throw new Error("snapshot.position.total: expected a number");
  }

  // =========================================================================
  // 🚧 lap  — object check done; field validation is your next task
  // =========================================================================
  if (typeof snap.lap !== "object" || snap.lap === null) {
    throw new Error("snapshot.lap: expected a non-null object");
  }
  const lap = snap.lap as Record<string, unknown>;

  if (typeof lap.number !== "number") {
    throw new Error("snapshot.lap.number: expected a number");
  }
  if (typeof lap.total !== "number") {
    throw new Error("snapshot.lap.total: expected a number");
  }
  if (typeof lap.liveDelta !== "number") {
    throw new Error("snapshot.lap.liveDelta: expected a number");
  }
  if (typeof lap.newLap !== "boolean") {
    throw new Error("snapshot.lap.newLap: expected boolean");
  }
  if (typeof lap.previousLapTime !== "number" && typeof lap.previousLapTime !== "undefined") {
    throw new Error("snapshot.lap.previousLapTime: expected a number");
  }
  if (typeof lap.bestLapTime !== "number") {
    throw new Error("snapshot.lap.bestLapTime: expected a number");
  }

  if (typeof lap.previousLapStanding !== "string" && typeof lap.previousLapStanding !== "undefined") {
    throw new Error("snapshot.lap.previousLapStanding: expected a string");
  }

  const previousLapStanding = lap.previousLapStanding as string
  switch (previousLapStanding) {
    case "gridFastest":
      break;
    case "personalBest":
      break;
    case undefined:
      break;
    default:
      throw new Error("snapshot.lap.previousLapStanding: can only be either of 'gridFastest' or 'personalBest'");
  }

  if (typeof lap.bestLapStanding !== "string") {
    throw new Error("snapshot.lap.bestLapStanding: expected a non-null string");
  }
  const bestLapStanding = lap.bestLapStanding as string
  switch (bestLapStanding) {
    case "gridFastest":
      break;
    case "personalBest":
      break;
    default:
      throw new Error("snapshot.lap.bestLapStanding: can only be either of 'gridFastest' or 'personalBest'");
  }

  // =========================================================================
  // ✅ sectors — array check + element object check done
  // =========================================================================
  if (!Array.isArray(snap.sectors)) {
    throw new Error("snapshot.sectors: expected an array");
  }
  for (const sector of snap.sectors as unknown[]) {
    if (typeof sector !== "object" || sector === null) {
      throw new Error("snapshot.sectors[]: expected a non-null object");
    }
    const s = sector as Record<string, unknown>;

    if (typeof s.number !== "number") {
      throw new Error("snapshot.sector.number: expected a number");
    }
    if (typeof s.liveDelta !== "number") {
      throw new Error("snapshot.sector.liveDelta: expected a number");
    }
    if (typeof s.newSector !== "boolean") {
      throw new Error("snapshot.sector.newSector: expected boolean");
    }
    if (typeof s.finishTime !== "number") {
      throw new Error("snapshot.sector.finishTime: expected a number");
    }

    if (typeof s.previousSectorStanding !== "string" && typeof s.previousSectorStanding !== "undefined") {
      throw new Error("snapshot.sector.previousSectorStanding: expected a string");
    }
    const previousSectorStanding = s.previousSectorStanding as string
    switch (previousSectorStanding) {
      case "gridFastest":
        break;
      case "personalBest":
        break;
      case undefined:
        break;
      default:
        throw new Error("snapshot.sector.previousSectorStanding: can only be either of 'gridFastest' or 'personalBest'");
    }
  }

  // =========================================================================
  // ✅ tyres — array check + element object check done
  // =========================================================================
  if (!Array.isArray(snap.tyres)) {
    throw new Error("snapshot.tyres: expected an array");
  }
  for (const tyre of snap.tyres as unknown[]) {
    if (typeof tyre !== "object" || tyre === null) {
      throw new Error("snapshot.tyres[]: expected a non-null object");
    }
    const t = tyre as Record<string, unknown>;

    if (typeof t.temp !== "number") {
      throw new Error("snapshot.tyre.temp: expected a number");
    }
    if (typeof t.wear !== "number") {
      throw new Error("snapshot.tyre.wear: expected a number");
    }

    if (typeof t.position !== "string") {
      throw new Error("snapshot.tyre.position: expected a non-null string");
    }
    const tpos = t.position as string
    switch (tpos) {
      case "FR":
        break;
      case "FL":
        break;
      case "RR":
        break;
      case "RL":
        break;
      default:
        throw new Error("snapshot.tyre.position: can only be either of 'FL', 'FR', 'RR' or 'RL'");
    }
    if (typeof t.compound !== "string") {
      throw new Error("snapshot.tyre.compound: expected a non-null string");
    }
    const compound = t.compound as string
    switch (compound) {
      case "RS":
        break;
      case "RM":
        break;
      case "RH":
        break;
      case "IM":
        break;
      case "W":
        break;
      default:
        throw new Error("snapshot.tyre.compound: can only be either of 'RS', 'RM', 'RH', 'IM', 'W'");
    }
  }

  // =========================================================================
  // 🚧 fuel — object check done; field validation is your next task
  // =========================================================================
  if (typeof snap.fuel !== "object" || snap.fuel === null) {
    throw new Error("snapshot.fuel: expected a non-null object");
  }
  const f = snap.fuel as Record<string, unknown>;

  if (typeof f.map !== "number") {
    throw new Error("snapshot.fuel.map: expected a number");
  }
  if (typeof f.remaining !== "number") {
    throw new Error("snapshot.fuel.remaining: expected a number");
  }
  if (typeof f.worthLap !== "number") {
    throw new Error("snapshot.fuel.worthLap: expected a number");
  }

  // =========================================================================
  // 🚧 coach — object check done; field validation is your next task
  // =========================================================================
  if (typeof snap.coach !== "object" || snap.coach === null) {
    throw new Error("snapshot.coach: expected a non-null object");
  }
  const c = snap.coach as Record<string, unknown>;

  if (typeof c.problem !== "string") {
    throw new Error("snapshot.coach.problem: expected a string");
  }
  if (typeof c.action !== "string") {
    throw new Error("snapshot.coach.action: expected a string");
  }
  if (typeof c.expectedGain !== "number") {
    throw new Error("snapshot.coach.expectedGain: expected a number");
  }


  if (typeof c.turn !== "object" || c.turn === null) {
    throw new Error("snapshot.coach.turn: expected a non-null object");
  }
  const turn = c.turn as Record<string, unknown>

  if (typeof turn.count !== "number") {
    throw new Error("snapshot.coach.turn.count: expected a number");
  }
  if (typeof turn.name !== "string" && typeof turn.name !== "undefined") {
    throw new Error("snapshot.coach.turn.name: expected a string");
  }

  return snap as DashboardSnapshot;
}
