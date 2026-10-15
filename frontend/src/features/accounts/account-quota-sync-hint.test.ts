import assert from "node:assert/strict";
import { describe, it } from "node:test";

import { quotaSyncHintText } from "./account-quota-sync-hint.ts";

const t = (key: string, o?: Record<string, unknown>) => `${key.split(".").pop()}${o?.time ? `(${String(o.time)})` : ""}`;
const clock = (v: string) => v;

describe("quotaSyncHintText", () => {
  it("shows last and hourly next", () => {
    assert.equal(quotaSyncHintText({ lastQuotaSyncAt: "a", nextQuotaSyncAt: "b", nextQuotaSyncReason: "hourly" }, t, clock), "last(a) · next(b)");
  });
  it("marks reset-triggered refresh", () => {
    assert.equal(quotaSyncHintText({ nextQuotaSyncAt: "b", nextQuotaSyncReason: "reset" }, t, clock), "never · nextReset(b)");
  });
  it("shows skip reasons", () => {
    assert.equal(quotaSyncHintText({ lastQuotaSyncAt: "a", nextQuotaSyncReason: "accountDisabled" }, t, clock), "last(a) · accountDisabled");
    assert.equal(quotaSyncHintText({ nextQuotaSyncReason: "autoSyncDisabled" }, t, clock), "never · autoSyncDisabled");
  });
});
