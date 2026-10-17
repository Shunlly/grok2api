import assert from "node:assert/strict";
import test from "node:test";

import { applyBoundAccountSelection } from "./bound-accounts.ts";

test("select all adds visible accounts without duplicates and keeps hidden picks", () => {
  assert.deepEqual(applyBoundAccountSelection(["9", "2"], ["1", "2", "3"], true), ["9", "2", "1", "3"]);
});

test("clear removes only visible accounts", () => {
  assert.deepEqual(applyBoundAccountSelection(["9", "2", "1"], ["1", "2", "3"], false), ["9"]);
  assert.deepEqual(applyBoundAccountSelection([], ["1"], false), []);
});
