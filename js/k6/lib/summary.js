// @ts-check
// Turns k6's end-of-test data into the small, versioned JSON the Go runner reads.

import { SLO } from "./profiles.js";

/** Schema id; the Go side refuses any other value. */
export const SCHEMA = "caplab.k6-summary.v1";

/**
 * @typedef {{ type?: string, values?: Record<string, number>, thresholds?: Record<string, { ok: boolean }> }} K6Metric
 * @typedef {{ metrics: Record<string, K6Metric>, state?: { testRunDurationMs?: number } }} K6Data
 * @typedef {import("./profiles.js").Plan} Plan
 */

/**
 * Round to three decimals so the JSON stays readable.
 * @param {number} v @returns {number}
 */
const r3 = (v) => Math.round(v * 1000) / 1000;

/**
 * Read one value of a metric, or a fallback when the metric never received a sample.
 * @param {K6Data} data @param {string} name @param {string} key @param {number | null} fallback
 * @returns {number}
 */
export function metricValue(data, name, key, fallback = null) {
  const v = data.metrics[name]?.values?.[key];
  if (typeof v === "number" && Number.isFinite(v)) return v;
  if (fallback !== null) return fallback;
  throw new Error(`k6 summary has no ${name} ${key}`);
}

/**
 * True when every threshold on the metric passed (or it has none).
 * @param {K6Data} data @param {string} name @returns {boolean}
 */
function thresholdsOk(data, name) {
  return Object.values(data.metrics[name]?.thresholds ?? {}).every((t) => t.ok);
}

/**
 * Latency percentiles of a trend metric, in milliseconds; zeros when it has no samples.
 * @param {K6Data} data @param {string} name
 */
function latency(data, name) {
  return {
    p50Ms: r3(metricValue(data, name, "med", 0)),
    p95Ms: r3(metricValue(data, name, "p(95)", 0)),
    p99Ms: r3(metricValue(data, name, "p(99)", 0)),
    maxMs: r3(metricValue(data, name, "max", 0)),
  };
}

/**
 * Build the versioned summary from k6's handleSummary data and the plan that drove the run.
 * @param {K6Data} data @param {Plan} plan
 */
export function compactSummary(data, plan) {
  const scenarios = plan.windows.map((w) => {
    const tag = `{scenario:${w.name}}`;
    const names = ["http_req_duration", "http_req_failed", "dropped_iterations", "iterations"].map((m) => m + tag);
    return {
      name: w.name,
      iterations: metricValue(data, `iterations${tag}`, "count", 0),
      failedRate: r3(metricValue(data, `http_req_failed${tag}`, "rate", 0)),
      droppedIterations: metricValue(data, `dropped_iterations${tag}`, "count", 0),
      ...latency(data, `http_req_duration${tag}`),
      thresholdsOk: names.every((n) => thresholdsOk(data, n)),
    };
  });
  return {
    schema: SCHEMA,
    kind: plan.params.kind,
    scale: plan.params.scale,
    label: plan.label,
    slo: { ...SLO },
    windows: plan.windows.map((w) => ({ ...w })),
    testRunDurationMs: Math.round(data.state?.testRunDurationMs ?? 0),
    totals: {
      requests: metricValue(data, "http_reqs", "count"),
      failedRate: r3(metricValue(data, "http_req_failed", "rate")),
      droppedIterations: metricValue(data, "dropped_iterations", "count", 0),
      shedResponses: metricValue(data, "shed_responses", "count", 0),
      transportErrors: metricValue(data, "transport_errors", "count", 0),
      vusMax: metricValue(data, "vus_max", "max", 0),
      ...latency(data, "http_req_duration"),
    },
    scenarios,
  };
}

/** @typedef {ReturnType<typeof compactSummary>} Summary */

/**
 * A few plain lines for the console, one per window.
 * @param {Summary} s @returns {string}
 */
export function summaryLines(s) {
  const pct = (/** @type {number} */ v) => `${(v * 100).toFixed(2)}%`;
  const lines = [
    `${s.label}`,
    `total: ${s.totals.requests} requests, failed ${pct(s.totals.failedRate)}, p95 ${s.totals.p95Ms} ms, dropped ${s.totals.droppedIterations}, max VUs ${s.totals.vusMax}`,
  ];
  for (const sc of s.scenarios) {
    const w = s.windows.find((x) => x.name === sc.name);
    const verdict = sc.thresholdsOk ? "meets SLO" : "misses SLO";
    lines.push(
      `${sc.name.padEnd(8)} ${String(w?.rate ?? "?").padStart(6)} req/s  p95 ${String(sc.p95Ms).padStart(8)} ms  failed ${pct(sc.failedRate).padStart(7)}  dropped ${String(sc.droppedIterations).padStart(5)}  ${verdict}${w?.gate ? " (gated)" : ""}`,
    );
  }
  return lines.join("\n") + "\n";
}
