import assert from "node:assert/strict";
import { describe, it } from "node:test";

import { formatUsageCompact, formatUsageUSD, isTodayCurrent, shanghaiDate } from "./account-usage-stats-format.ts";

describe("account usage stats format", () => {
  it("uses the Asia/Shanghai calendar day", () => {
    assert.equal(shanghaiDate(new Date("2026-10-14T16:30:00Z")), "2026-10-15");
    assert.equal(shanghaiDate(new Date("2026-10-14T15:59:00Z")), "2026-10-14");
  });
  it("treats a snapshot from another day as stale", () => {
    const now = new Date("2026-10-15T06:00:00Z");
    assert.equal(isTodayCurrent("2026-10-15", now), true);
    assert.equal(isTodayCurrent("2026-10-14", now), false);
    assert.equal(isTodayCurrent(undefined, now), false);
  });
  it("formats compact numbers and ticks", () => {
    assert.equal(formatUsageCompact(17_500), "17.5K");
    assert.equal(formatUsageCompact(1_600_000_000), "1.6B");
    assert.equal(formatUsageUSD(13_352_000_000_000), "$1335.20");
    assert.equal(formatUsageUSD(0), "$0.00");
  });
});
