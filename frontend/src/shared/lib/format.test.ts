import assert from "node:assert/strict";
import { describe, it } from "node:test";

import { formatRelativeFuture, formatResetClock, formatTokenMillions } from "./format.ts";

describe("formatTokenMillions", () => {
  it("uses a stable M suffix for large values", () => {
    assert.equal(formatTokenMillions(0, "en"), "0");
    assert.equal(formatTokenMillions(999, "en"), "999");
    assert.equal(formatTokenMillions(1_000, "en"), "0.001M");
    assert.equal(formatTokenMillions(500_000, "en"), "0.5M");
    assert.equal(formatTokenMillions(1_000_000, "en"), "1M");
    assert.equal(formatTokenMillions(1_234_567, "en"), "1.23M");
  });

  it("normalizes invalid and negative values", () => {
    assert.equal(formatTokenMillions(Number.NaN, "en"), "0");
    assert.equal(formatTokenMillions(-10, "en"), "0");
  });
});

describe("formatRelativeFuture", () => {
  const now = new Date("2026-10-15T04:00:00Z");
  it("picks minute/hour/day units", () => {
    assert.equal(formatRelativeFuture(new Date("2026-10-15T04:30:00Z"), "en", now), "in 30 min.");
    assert.equal(formatRelativeFuture(new Date("2026-10-15T09:00:00Z"), "en", now), "in 5 hr.");
    assert.equal(formatRelativeFuture(new Date("2026-10-17T04:00:00Z"), "en", now), "in 2 days");
    assert.equal(formatRelativeFuture("bad", "en", now), "-");
  });
  it("formats a local clock", () => {
    const local = new Date(2026, 9, 17, 8, 5);
    assert.equal(formatResetClock(local), "10-17 08:05");
    assert.equal(formatResetClock("bad"), "-");
  });
});
