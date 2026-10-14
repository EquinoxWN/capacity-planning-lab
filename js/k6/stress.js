// @ts-check
// Stress test: step the arrival rate up until the service breaks its SLO.
// Run through `caplab run stress`, or directly: k6 run -e BASE_URL=http://127.0.0.1:8080 stress.js

import { makeTest } from "./lib/scenario.js";

const test = makeTest("stress");

export const options = test.options;
export const handleSummary = test.handleSummary;

/** One request to /work. */
export default function () {
  test.iteration();
}
