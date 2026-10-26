import assert from "node:assert/strict";
import test from "node:test";

import { summarizeProbe } from "./egress-probe-summary.ts";

test("both families healthy is a success", () => {
  const value = summarizeProbe({ status: "healthy", exitIp: "1.2.3.4", ipv4: { status: "healthy" }, ipv6: { status: "healthy" } });
  assert.equal(value.kind, "success");
  assert.equal(value.exitIp, "1.2.3.4");
});

test("ipv4 only is a usable warning, not a failure", () => {
  const value = summarizeProbe({ status: "unhealthy", ipv4: { status: "healthy", exitIp: "192.46.187.40" }, ipv6: { status: "unhealthy", error: "代理连接失败" } });
  assert.equal(value.kind, "warning");
  assert.equal(value.exitIp, "192.46.187.40");
  assert.equal(value.ipv6, "unhealthy");
});

test("no healthy family is an error with the upstream message", () => {
  const value = summarizeProbe({ status: "unhealthy", error: "socks 认证失败", ipv4: { status: "unhealthy" }, ipv6: { status: "unhealthy" } });
  assert.equal(value.kind, "error");
  assert.equal(value.error, "socks 认证失败");
});
