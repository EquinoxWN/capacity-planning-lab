// @ts-check
// Load test: ramp up, then hold the planned rate and check the SLO.
// Run through `caplab run load`, or directly: k6 run -e BASE_URL=http://127.0.0.1:8080 load.js

import { makeTest } from "./lib/scenario.js";

const test = makeTest("load");

export const options = test.options;
export const handleSummary = test.handleSummary;

/** One request to /work. */
export default function () {
  test.iteration();
}
