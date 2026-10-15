import assert from "node:assert/strict";
import { describe, it } from "node:test";

import { displayTimeZoneLabel, formatCompactDateTime, formatRelativeFuture, formatResetClock, formatTokenMillions } from "./format.ts";

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
  it("formats in the UTC+8 display zone regardless of the browser zone", () => {
    // Regression: a browser on US Pacific showed 08:58Z as "10-15 01:58 · 4小时后" at 13:00 UTC+8.
    assert.equal(formatResetClock("2026-10-15T08:58:56Z"), "10-15 16:58");
    assert.equal(formatResetClock(new Date("2026-10-17T00:05:00Z")), "10-17 08:05");
    assert.equal(formatResetClock("2026-10-15T16:30:00Z"), "10-16 00:30");
    assert.equal(formatCompactDateTime("2026-10-15T08:58:56Z", "zh-CN"), "2026/10/15 16:58:56");
    assert.equal(displayTimeZoneLabel(), "UTC+8");
    assert.equal(formatResetClock("bad"), "-");
  });
});
