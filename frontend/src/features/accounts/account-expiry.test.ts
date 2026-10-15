import assert from "node:assert/strict";
import { describe, it } from "node:test";

import { expiryState } from "./account-expiry.ts";

describe("expiryState", () => {
  const now = new Date("2026-10-15T04:00:00Z");
  it("classifies expiry", () => {
    assert.equal(expiryState(undefined, false, now), "unknown");
    assert.equal(expiryState("2026-10-15T03:00:00Z", false, now), "expired");
    assert.equal(expiryState("2026-10-16T04:00:00Z", false, now), "soon");
    assert.equal(expiryState("2026-10-20T04:00:00Z", false, now), "ok");
    assert.equal(expiryState("2026-10-15T05:00:00Z", true, now), "ok");
  });
});
