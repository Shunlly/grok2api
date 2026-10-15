import assert from "node:assert/strict";
import { describe, it } from "node:test";

import { allowedModelOptions } from "./allowed-model-options.ts";

describe("allowedModelOptions", () => {
  it("merges upstream list, routed models and existing selection", () => {
    const options = allowedModelOptions({
      all: [{ id: "b", publicIds: ["B"] }],
      usable: [{ id: "A", upstreamModel: "a" }],
      unavailable: [{ id: "Off", upstreamModel: "off", reason: "routeDisabled" }, { id: "B2", upstreamModel: "b", reason: "notAllowed" }],
    }, ["legacy"]);
    assert.deepEqual(options, [
      { id: "a", publicIds: ["A"] },
      { id: "b", publicIds: ["B", "B2"] },
      { id: "legacy", publicIds: [] },
    ]);
  });
});
