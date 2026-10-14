// @ts-check
// The k6 side of every test: one GET /work per iteration, counted by outcome.

import http from "k6/http";
import { Counter } from "k6/metrics";
import { buildPlan, LIMITS, loopbackBase, parseParams } from "./profiles.js";
import { compactSummary, summaryLines } from "./summary.js";

// Custom counters must be created in the init context.
const shed = new Counter("shed_responses");
const transport = new Counter("transport_errors");

/**
 * Build options, the iteration function and handleSummary for one test kind.
 * @param {string} kind
 */
export function makeTest(kind) {
  const plan = buildPlan(parseParams(kind, __ENV));
  const url = `${loopbackBase(__ENV.BASE_URL)}/work`;
  const summaryPath = __ENV.SUMMARY_PATH ?? "";
  if (summaryPath !== "" && !/^[^\0]{1,500}\.json$/.test(summaryPath)) throw new Error("SUMMARY_PATH must name a .json file");

  /** @type {import("k6/options").Options} */
  const options = {
    scenarios: /** @type {Record<string, import("k6/options").Scenario>} */ (plan.scenarios),
    thresholds: plan.thresholds,
    discardResponseBodies: true,
    summaryTrendStats: ["avg", "min", "med", "max", "p(90)", "p(95)", "p(99)"],
    userAgent: "caplab-k6/1",
  };

  /** One request; 503 and transport failures are counted separately. */
  function iteration() {
    const res = http.get(url, { timeout: LIMITS.requestTimeout, tags: { name: "GET /work" } });
    if (res.status === 503) shed.add(1);
    else if (res.status === 0) transport.add(1);
  }

  /**
   * Write the versioned summary for the Go runner and print a short table.
   * @param {import("./summary.js").K6Data} data
   */
  function handleSummary(data) {
    const s = compactSummary(data, plan);
    /** @type {Record<string, string>} */
    const out = { stdout: summaryLines(s) };
    if (summaryPath !== "") out[summaryPath] = JSON.stringify(s, null, 2) + "\n";
    return out;
  }

  return { options, iteration, handleSummary };
}
