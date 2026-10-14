// @ts-check
import assert from "node:assert/strict";
import test from "node:test";
import { buildPlan, DEFAULTS, intEnv, LIMITS, loopbackBase, parseParams, SLO, vusFor } from "../k6/lib/profiles.js";

const KINDS = /** @type {const} */ (["load", "stress", "spike", "soak"]);

test("every kind builds a plan with back-to-back windows at both scales", () => {
  for (const scale of ["full", "quick"]) {
    for (const kind of KINDS) {
      const plan = buildPlan(parseParams(kind, { SCALE: scale }));
      assert.ok(plan.windows.length >= 2, `${kind}/${scale} has windows`);
      let t = 0;
      for (const w of plan.windows) {
        assert.equal(w.startS, t, `${kind}/${scale} ${w.name} starts where the previous ended`);
        assert.ok(w.endS > w.startS && w.rate > 0);
        t = w.endS;
      }
      assert.equal(plan.totalS, t);
      assert.ok(plan.totalS <= LIMITS.maxTotalS, `${kind}/${scale} stays under ${LIMITS.maxTotalS} s`);
      assert.deepEqual(Object.keys(plan.scenarios), plan.windows.map((w) => w.name));
    }
  }
});

test("quick plans are shorter than full plans (CI variant)", () => {
  for (const kind of KINDS) {
    const full = buildPlan(parseParams(kind, {})).totalS;
    const quick = buildPlan(parseParams(kind, { SCALE: "quick" })).totalS;
    assert.ok(quick < full, `${kind}: quick ${quick} s < full ${full} s`);
    assert.ok(quick <= 60, `${kind}: quick run is at most a minute`);
  }
});

test("every window gets SLO thresholds tagged by its scenario", () => {
  const plan = buildPlan(parseParams("stress", {}));
  for (const w of plan.windows) {
    const tag = `{scenario:${w.name}}`;
    assert.deepEqual(plan.thresholds[`http_req_duration${tag}`], [`p(95)<=${SLO.p95Ms}`]);
    assert.deepEqual(plan.thresholds[`http_req_failed${tag}`], [`rate<=${SLO.errorRate}`]);
    const allowed = Math.max(1, Math.floor(w.rate * (w.endS - w.startS) * SLO.droppedRate));
    assert.deepEqual(plan.thresholds[`dropped_iterations${tag}`], [`count<=${allowed}`]);
    assert.deepEqual(plan.thresholds[`iterations${tag}`], ["count>0"]);
  }
});

test("stress steps are open-model, ungated and strictly increasing", () => {
  const plan = buildPlan(parseParams("stress", {}));
  assert.deepEqual(plan.windows.map((w) => w.rate), DEFAULTS.full.steps);
  for (const w of plan.windows) {
    const sc = /** @type {Record<string, unknown>} */ (plan.scenarios[w.name]);
    assert.equal(sc.executor, "constant-arrival-rate", "arrival-rate executors avoid coordinated omission");
    assert.equal(sc.rate, w.rate);
    assert.equal(sc.startTime, `${w.startS}s`);
    assert.equal(w.gate, false, "stress steps are expected to fail past the breaking point");
  }
});

test("load, spike and soak gate the right windows", () => {
  const gated = (/** @type {string} */ kind) => buildPlan(parseParams(kind, {})).windows.filter((w) => w.gate).map((w) => w.name);
  assert.deepEqual(gated("load"), ["load"]);
  assert.deepEqual(gated("spike"), ["before", "after"]);
  assert.deepEqual(gated("soak"), ["soak"]);
  const load = buildPlan(parseParams("load", { RATE: "300" }));
  const warm = /** @type {Record<string, unknown>} */ (load.scenarios.warmup);
  assert.equal(warm.executor, "ramping-arrival-rate");
  assert.equal(warm.startRate, 30);
});

test("the spike peak defaults to twice the rate and must exceed it", () => {
  const plan = buildPlan(parseParams("spike", { RATE: "500" }));
  assert.deepEqual(plan.windows.map((w) => [w.name, w.rate]), [["before", 500], ["spike", 1000], ["after", 500]]);
  assert.throws(() => parseParams("spike", { RATE: "500", PEAK: "400" }), /PEAK \(400\) must be above RATE/);
});

test("the soak is labelled as shortened", () => {
  const plan = buildPlan(parseParams("soak", {}));
  assert.match(plan.label, /^shortened soak: .* \(a production soak runs for hours\)$/);
});

test("environment input is validated", () => {
  assert.equal(intEnv("X", undefined, 7, 1, 10), 7);
  assert.equal(intEnv("X", "", 7, 1, 10), 7);
  for (const bad of ["-1", "1.5", "1e3", " 5", "abc", "1234567"]) {
    assert.throws(() => intEnv("X", bad, 7, 1, 10_000_000), /whole number/, bad);
  }
  assert.throws(() => intEnv("X", "11", 7, 1, 10), /between 1 and 10/);
  assert.throws(() => parseParams("chaos", {}), /unknown test kind/);
  assert.throws(() => parseParams("load", { SCALE: "huge" }), /SCALE must be/);
  assert.throws(() => parseParams("load", { RATE: "0" }), /RATE must be between/);
  assert.throws(() => parseParams("load", { RATE: String(LIMITS.maxRate + 1) }), /RATE must be between/);
  assert.throws(() => parseParams("stress", { STEPS: "100,100" }), /strictly increasing/);
  assert.throws(() => parseParams("stress", { STEPS: "100" }), /2 to 12 rates/);
  assert.throws(() => parseParams("stress", { STEPS: "1,2,3,4,5,6,7,8,9,10,11,12,13" }), /2 to 12 rates/);
  assert.throws(() => parseParams("soak", { HOLD_S: "1" }), /HOLD_S must be between/);
  assert.deepEqual(parseParams("stress", { STEPS: "10, 20,30" }).steps, [10, 20, 30]);
});

test("plans longer than the shared-machine limit are refused", () => {
  const steps = Array.from({ length: 12 }, (_, i) => String((i + 1) * 10)).join(",");
  assert.throws(() => buildPlan(parseParams("stress", { STEPS: steps, HOLD_S: "60" })), /over the 600 s limit/);
});

test("VU pools grow with the rate and stay capped", () => {
  const small = vusFor(10);
  assert.equal(small.preAllocatedVUs, small.maxVUs, "all VUs start before the test");
  const big = vusFor(LIMITS.maxRate);
  assert.equal(big.maxVUs, LIMITS.maxVUs);
  assert.ok(big.preAllocatedVUs <= big.maxVUs);
  assert.ok(vusFor(1000).maxVUs > vusFor(100).maxVUs);
});

test("only loopback base URLs are accepted", () => {
  for (const ok of ["http://127.0.0.1:8080", "http://localhost:1", "http://[::1]:65535"]) {
    assert.equal(loopbackBase(ok), ok);
  }
  for (const bad of [undefined, "", "http://10.0.0.1:80", "https://127.0.0.1:443", "http://127.0.0.1", "http://127.0.0.1:8080/", "http://127.0.0.1:0", "http://127.0.0.1:70000", "http://127.0.0.1.evil.com:80"]) {
    assert.throws(() => loopbackBase(bad), /BASE_URL must be/, String(bad));
  }
});
