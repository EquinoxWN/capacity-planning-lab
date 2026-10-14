// @ts-check
// Load profiles for the four test kinds. Pure functions: no k6 imports, so Node tests can run them.

/** Service-level objective every gated window is judged against. */
export const SLO = Object.freeze({ p95Ms: 50, errorRate: 0.01, droppedRate: 0.01 });

/** Hard limits that keep a run short and light enough for a shared laptop. */
export const LIMITS = Object.freeze({
  maxRate: 20000,
  maxTotalS: 600,
  minHoldS: 3,
  maxHoldS: 300,
  maxSteps: 12,
  maxVUs: 400,
  requestTimeout: "2s",
});

/** Default rates and durations; `quick` is the CI variant of every profile. */
export const DEFAULTS = Object.freeze({
  full: { rate: 600, steps: [400, 700, 1000, 1300, 1600, 1900, 2200, 2500], peakFactor: 2, holdS: { load: 60, stress: 15, spike: 10, soak: 180 } },
  quick: { rate: 200, steps: [200, 400, 800, 1600, 3200, 6400, 12800], peakFactor: 2, holdS: { load: 10, stress: 5, spike: 5, soak: 20 } },
});

/** @typedef {"load" | "stress" | "spike" | "soak"} Kind */
/** @typedef {"full" | "quick"} Scale */
/**
 * @typedef {object} Params
 * @property {Kind} kind
 * @property {Scale} scale
 * @property {number} rate      requests per second for load, soak and the spike baseline
 * @property {number[]} steps   stress step rates, strictly increasing
 * @property {number} peak      spike peak rate
 * @property {number} holdS     main hold duration in seconds (per step for stress)
 */
/**
 * @typedef {object} Window
 * @property {string} name   k6 scenario name
 * @property {number} rate   target arrival rate (requests per second)
 * @property {number} startS start, in seconds after the test starts
 * @property {number} endS   end, in seconds after the test starts
 * @property {boolean} gate  true when the window must meet the SLO for the run to pass
 */
/**
 * @typedef {object} Plan
 * @property {Params} params
 * @property {Window[]} windows
 * @property {Record<string, object>} scenarios
 * @property {Record<string, string[]>} thresholds
 * @property {number} totalS
 * @property {string} label
 */

const KINDS = /** @type {const} */ (["load", "stress", "spike", "soak"]);

/**
 * Parse a positive integer from an environment value, within [lo, hi].
 * @param {string} name @param {string | undefined} raw @param {number} fallback @param {number} lo @param {number} hi
 * @returns {number}
 */
export function intEnv(name, raw, fallback, lo, hi) {
  if (raw === undefined || raw === "") return fallback;
  if (!/^\d{1,6}$/.test(raw)) throw new Error(`${name} must be a whole number, got ${JSON.stringify(raw)}`);
  const v = Number(raw);
  if (v < lo || v > hi) throw new Error(`${name} must be between ${lo} and ${hi}, got ${v}`);
  return v;
}

/**
 * Read and validate the parameters of one test kind from environment variables.
 * @param {string} kind @param {Record<string, string | undefined>} env
 * @returns {Params}
 */
export function parseParams(kind, env) {
  if (!KINDS.includes(/** @type {Kind} */ (kind))) throw new Error(`unknown test kind ${JSON.stringify(kind)}`);
  const k = /** @type {Kind} */ (kind);
  const scaleRaw = env.SCALE ?? "full";
  if (scaleRaw !== "full" && scaleRaw !== "quick") throw new Error(`SCALE must be full or quick, got ${JSON.stringify(scaleRaw)}`);
  /** @type {Scale} */
  const scale = scaleRaw;
  const d = DEFAULTS[scale];
  const rate = intEnv("RATE", env.RATE, d.rate, 1, LIMITS.maxRate);
  let steps = [...d.steps];
  if (env.STEPS !== undefined && env.STEPS !== "") {
    steps = env.STEPS.split(",").map((s, i) => intEnv(`STEPS[${i}]`, s.trim(), 0, 1, LIMITS.maxRate));
    if (steps.length < 2 || steps.length > LIMITS.maxSteps) throw new Error(`STEPS needs 2 to ${LIMITS.maxSteps} rates, got ${steps.length}`);
    for (let i = 1; i < steps.length; i++) {
      if (/** @type {number} */ (steps[i]) <= /** @type {number} */ (steps[i - 1])) throw new Error("STEPS must be strictly increasing");
    }
  }
  const peak = intEnv("PEAK", env.PEAK, Math.min(rate * d.peakFactor, LIMITS.maxRate), 1, LIMITS.maxRate);
  if (k === "spike" && peak <= rate) throw new Error(`PEAK (${peak}) must be above RATE (${rate}) for a spike`);
  const holdS = intEnv("HOLD_S", env.HOLD_S, d.holdS[k], LIMITS.minHoldS, LIMITS.maxHoldS);
  return { kind: k, scale, rate, steps, peak, holdS };
}

/**
 * VU pool for an arrival rate: every VU is started before the test (k6 drops iterations while it
 * starts extra VUs mid-run), enough for ~250 ms per request plus 20 (the 100 ms queue wait plus
 * work and interference), capped for memory.
 * @param {number} rate @returns {{ preAllocatedVUs: number, maxVUs: number }}
 */
export function vusFor(rate) {
  const vus = Math.min(LIMITS.maxVUs, Math.ceil(rate * 0.25) + 20);
  return { preAllocatedVUs: vus, maxVUs: vus };
}

/**
 * A constant-arrival-rate scenario for one window.
 * @param {Window} w @returns {object}
 */
function constant(w) {
  return {
    executor: "constant-arrival-rate",
    rate: w.rate,
    timeUnit: "1s",
    duration: `${w.endS - w.startS}s`,
    startTime: `${w.startS}s`,
    gracefulStop: "3s",
    ...vusFor(w.rate),
  };
}

/**
 * A ramping-arrival-rate warm-up from a tenth of the window's rate up to it.
 * @param {Window} w @returns {object}
 */
function ramp(w) {
  return {
    executor: "ramping-arrival-rate",
    startRate: Math.max(1, Math.ceil(w.rate / 10)),
    timeUnit: "1s",
    stages: [{ target: w.rate, duration: `${w.endS - w.startS}s` }],
    startTime: `${w.startS}s`,
    gracefulStop: "3s",
    ...vusFor(w.rate),
  };
}

/**
 * Lay out the windows of a test kind back to back.
 * @param {Params} p @returns {{ windows: Window[], label: string }}
 */
export function windowsFor(p) {
  /** @type {Window[]} */
  const out = [];
  let t = 0;
  /** @param {string} name @param {number} rate @param {number} dur @param {boolean} gate */
  const add = (name, rate, dur, gate) => {
    out.push({ name, rate, startS: t, endS: t + dur, gate });
    t += dur;
  };
  const warm = p.scale === "quick" ? 3 : 10;
  switch (p.kind) {
    case "load":
      add("warmup", p.rate, warm, false);
      add("load", p.rate, p.holdS, true);
      return { windows: out, label: `load test: ${p.rate} req/s for ${p.holdS} s after a ${warm} s warm-up` };
    case "stress":
      p.steps.forEach((r, i) => add(`step${String(i + 1).padStart(2, "0")}`, r, p.holdS, false));
      return { windows: out, label: `stress test: ${p.steps.length} steps of ${p.holdS} s from ${p.steps[0]} to ${p.steps.at(-1)} req/s` };
    case "spike":
      add("before", p.rate, 2 * p.holdS, true);
      add("spike", p.peak, p.holdS, false);
      add("after", p.rate, 3 * p.holdS, true);
      return { windows: out, label: `spike test: ${p.rate} -> ${p.peak} req/s for ${p.holdS} s -> ${p.rate} req/s` };
    case "soak":
      add("warmup", p.rate, warm, false);
      add("soak", p.rate, p.holdS, true);
      return {
        windows: out,
        label: `shortened soak: ${p.rate} req/s for ${p.holdS} s on a shared laptop (a production soak runs for hours)`,
      };
  }
}

/**
 * Build the k6 scenarios and per-window SLO thresholds for validated parameters.
 * @param {Params} p @returns {Plan}
 */
export function buildPlan(p) {
  const { windows, label } = windowsFor(p);
  /** @type {Record<string, object>} */
  const scenarios = {};
  /** @type {Record<string, string[]>} */
  const thresholds = {};
  for (const w of windows) {
    scenarios[w.name] = w.name === "warmup" ? ramp(w) : constant(w);
    const tag = `{scenario:${w.name}}`;
    const planned = w.rate * (w.endS - w.startS);
    thresholds[`http_req_duration${tag}`] = [`p(95)<=${SLO.p95Ms}`];
    thresholds[`http_req_failed${tag}`] = [`rate<=${SLO.errorRate}`];
    thresholds[`dropped_iterations${tag}`] = [`count<=${Math.max(1, Math.floor(planned * SLO.droppedRate))}`];
    thresholds[`iterations${tag}`] = ["count>0"];
  }
  const totalS = windows.at(-1)?.endS ?? 0;
  if (totalS > LIMITS.maxTotalS) throw new Error(`plan runs ${totalS} s, over the ${LIMITS.maxTotalS} s limit for a shared machine`);
  return { params: p, windows, scenarios, thresholds, totalS, label };
}

/**
 * Accept only an http URL on the loopback interface, without a trailing slash.
 * @param {string | undefined} raw @returns {string}
 */
export function loopbackBase(raw) {
  const m = /^http:\/\/(127\.0\.0\.1|localhost|\[::1\]):(\d{1,5})$/.exec(raw ?? "");
  if (!m || Number(m[2]) < 1 || Number(m[2]) > 65535) {
    throw new Error(`BASE_URL must be http://127.0.0.1:<port>, http://localhost:<port> or http://[::1]:<port>, got ${JSON.stringify(raw)}`);
  }
  return /** @type {string} */ (raw);
}
