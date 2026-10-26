import assert from "node:assert/strict";
import test from "node:test";

import { withPending } from "./pending-set.ts";

test("pending tests are tracked per node", () => {
  const one = withPending(new Set(), "1", true);
  const two = withPending(one, "2", true);
  assert.deepEqual([...two], ["1", "2"]);
  const done = withPending(two, "1", false);
  assert.equal(done.has("1"), false);
  assert.equal(done.has("2"), true, "finishing node 1 must not affect node 2");
});

test("unchanged state keeps the same set", () => {
  const value = withPending(new Set(), "1", true);
  assert.equal(withPending(value, "1", true), value);
  assert.equal(withPending(value, "3", false), value);
});
