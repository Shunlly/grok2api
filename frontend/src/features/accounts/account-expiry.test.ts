import assert from "node:assert/strict";
import { describe, it } from "node:test";

import { credentialIssues, expiryState } from "./account-expiry.ts";

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

describe("credentialIssues", () => {
  const now = new Date("2026-10-15T00:00:00Z");
  const base = { authType: "oauth", authStatus: "active", refreshable: true, refreshFailureCount: 0, expiresAt: "2026-10-15T05:00:00Z" };
  it("healthy oauth has no issues", () => assert.deepEqual(credentialIssues(base, now), []));
  it("flags missing refresh token, failures and expiry", () => {
    assert.deepEqual(credentialIssues({ ...base, refreshable: false }, now), ["noRefreshToken"]);
    assert.deepEqual(credentialIssues({ ...base, refreshFailureCount: 2 }, now), ["refreshFailed"]);
    assert.deepEqual(credentialIssues({ ...base, expiresAt: "2026-10-14T23:00:00Z" }, now), ["expired"]);
    assert.deepEqual(credentialIssues({ ...base, expiresAt: "2026-10-14T23:55:00Z" }, now), []);
  });
  it("sso only reports reauthRequired", () => {
    assert.deepEqual(credentialIssues({ authType: "sso", authStatus: "active", refreshable: false, refreshFailureCount: 0 }, now), []);
    assert.deepEqual(credentialIssues({ authType: "sso", authStatus: "reauthRequired", refreshable: false, refreshFailureCount: 0 }, now), ["reauthRequired"]);
  });
});
