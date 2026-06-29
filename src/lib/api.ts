import type { DashboardSnapshot } from "../contract/dashboard";
import { validate } from "./validate";

/**
 * fetchSnapshot — fetches one dashboard snapshot from the backend API.
 *
 * This is YOUR next implementation task after finishing validate.ts.
 *
 * The function signature is already defined for you:
 *
 *   async function fetchSnapshot(url: string): Promise<DashboardSnapshot>
 *
 * Before implementing, think through these questions with your tutor:
 *
 *   Q1: What is Promise<T> in Go terms? (Hint: think goroutine + result channel)
 *
 *   Q2: fetch() is a built-in browser function that makes HTTP requests.
 *       It returns a Promise<Response>. How would you write a Go equivalent
 *       in terms of what it blocks on and what it returns?
 *
 *   Q3: After fetch() resolves you get a Response object. It has:
 *         response.ok      — boolean, true for 2xx status codes
 *         response.json()  — returns Promise<unknown> (the parsed body)
 *       What should you do if response.ok is false?
 *
 *   Q4: response.json() gives you unknown. What function do you already have
 *       that turns unknown into a typed DashboardSnapshot?
 *
 * Once you've talked through those, implement the body below.
 */
export async function fetchSnapshot(url: string): Promise<DashboardSnapshot> {
  const response = await fetch(url);
  if (!response.ok) {
    throw new Error(`expected 200, got ${response.status}`);

  }
  const body = await response.json();
  return validate(body);
}
