import assert from "node:assert/strict";
import test from "node:test";

import { buildPoolOutage } from "./build-outage.ts";

const summary = (total: number, available: number) => ({
  total, available, recovering: 0, attention: 0, risk: 0,
  providers: { grok_build: { total, available }, grok_web: { total: 0, available: 0 }, grok_console: { total: 0, available: 0 } },
  recovery: { cooldown: 0, waitingReset: 0, probing: 0 },
  issues: { disabled: 0, reauthRequired: 0 },
});

test("alerts only when every Build account is unavailable", () => {
  assert.deepEqual(buildPoolOutage(summary(3, 0)), { total: 3, available: 0 });
  assert.equal(buildPoolOutage(summary(3, 1)), null);
  assert.equal(buildPoolOutage(summary(0, 0)), null);
  assert.equal(buildPoolOutage(undefined), null);
});
