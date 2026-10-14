// @ts-check
// Spike test: jump from the planned rate to a peak and back; check shedding and recovery.
// Run through `caplab run spike`, or directly: k6 run -e BASE_URL=http://127.0.0.1:8080 spike.js

import { makeTest } from "./lib/scenario.js";

const test = makeTest("spike");

export const options = test.options;
export const handleSummary = test.handleSummary;

/** One request to /work. */
export default function () {
  test.iteration();
}
