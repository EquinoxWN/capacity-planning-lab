// @ts-check
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { buildPlan, parseParams } from "../k6/lib/profiles.js";
import { compactSummary, metricValue, SCHEMA, summaryLines } from "../k6/lib/summary.js";

/** @typedef {import("../k6/lib/summary.js").K6Data} K6Data */

// Recorded with k6 v2.3.0: `k6 run -e SCALE=quick -e STEPS=100,200,400,800,1600 stress.js` against `caplab serve`; the service
// was started a moment after k6, so step01 holds 78 real connection failures.
const raw = /** @type {K6Data} */ (JSON.parse(readFileSync(new URL("./fixtures/k6-data.stress-quick.json", import.meta.url), "utf8")));
// The summary k6 itself wrote from that data during the same run: the contract the Go side parses.
const contract = JSON.parse(readFileSync(new URL("../../testdata/contract/summary-v1.json", import.meta.url), "utf8"));
// The recorded run used the earlier quick steps, passed explicitly here.
const plan = buildPlan(parseParams("stress", { SCALE: "quick", STEPS: "100,200,400,800,1600" }));

test("Node reproduces the summary k6 wrote from the same data (one contract, two runtimes)", () => {
  assert.deepEqual(compactSummary(raw, plan), contract);
});

test("the recorded run's numbers come through unchanged", () => {
  const s = compactSummary(raw, plan);
  assert.equal(s.schema, SCHEMA);
  assert.equal(s.totals.requests, 15504);
  assert.equal(s.totals.transportErrors, 78);
  assert.equal(s.totals.shedResponses, 0, "no 503 in this run, so the counter is absent and reads as 0");
  const step01 = s.scenarios[0];
  assert.ok(step01);
  assert.equal(step01.failedRate, 0.156);
  assert.equal(step01.thresholdsOk, false, "step01 broke its error-rate threshold");
  assert.ok(s.scenarios.slice(1).every((sc) => sc.thresholdsOk));
  assert.deepEqual(s.scenarios.map((sc) => sc.iterations), [500, 1001, 2001, 4001, 8001]);
});

test("missing required metrics fail loudly; optional ones default", () => {
  assert.throws(() => compactSummary({ metrics: {} }, plan), /no http_reqs count/);
  assert.equal(metricValue({ metrics: {} }, "shed_responses", "count", 0), 0);
  assert.throws(() => metricValue({ metrics: { x: { values: { count: Number.NaN } } } }, "x", "count"), /no x count/);
});

test("a failing threshold marks only its own window", () => {
  /** @type {K6Data} */
  const data = structuredClone(raw);
  const m = data.metrics["http_req_duration{scenario:step04}"];
  assert.ok(m?.thresholds);
  m.thresholds["p(95)<=50"] = { ok: false };
  const ok = compactSummary(data, plan).scenarios.map((sc) => sc.thresholdsOk);
  assert.deepEqual(ok, [false, true, true, false, true]);
});

test("console lines name every window and its verdict", () => {
  const text = summaryLines(compactSummary(raw, plan));
  assert.match(text, /^stress test: 5 steps of 5 s from 100 to 1600 req\/s\n/);
  assert.match(text, /step01 +100 req\/s .* misses SLO\n/);
  assert.match(text, /step05 +1600 req\/s .* meets SLO\n$/);
});
