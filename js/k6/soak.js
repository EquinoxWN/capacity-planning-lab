// @ts-check
// Soak test (shortened for a shared laptop): hold the planned rate and watch for drift.
// Run through `caplab run soak`, or directly: k6 run -e BASE_URL=http://127.0.0.1:8080 soak.js

import { makeTest } from "./lib/scenario.js";

const test = makeTest("soak");

export const options = test.options;
export const handleSummary = test.handleSummary;

/** One request to /work. */
export default function () {
  test.iteration();
}
