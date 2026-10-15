import assert from "node:assert/strict";
import test from "node:test";

import { isChunkLoadError } from "./chunk-reload.ts";

test("detects stale dynamic import failures", () => {
  assert.equal(isChunkLoadError(new TypeError("Failed to fetch dynamically imported module: http://x/assets/models-page-CQGdagIn.js")), true);
  assert.equal(isChunkLoadError(new TypeError("Importing a module script failed.")), true);
  assert.equal(isChunkLoadError(new Error("error loading dynamically imported module")), true);
  assert.equal(isChunkLoadError(new Error("Cannot read properties of undefined")), false);
  assert.equal(isChunkLoadError(undefined), false);
});
