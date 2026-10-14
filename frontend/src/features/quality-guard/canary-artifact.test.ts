import assert from "node:assert/strict";
import { test } from "node:test";

import { buildSandboxedDocument, extractRenderableMarkup } from "./canary-artifact.ts";

test("extracts html from fenced answer", () => {
  const markup = extractRenderableMarkup("Here:\n```html\n<html><body><svg viewBox=\"0 0 1 1\"></svg></body></html>\n```");
  assert.equal(markup, "<html><body><svg viewBox=\"0 0 1 1\"></svg></body></html>");
});

test("extracts bare svg and returns null for plain text", () => {
  assert.equal(extractRenderableMarkup("x <svg><circle/></svg> y"), "<svg><circle/></svg>");
  assert.equal(extractRenderableMarkup("Final answer: 401"), null);
});

test("sandboxed document strips scripts, handlers and external refs", () => {
  const doc = buildSandboxedDocument('<svg onload="alert(1)"><script>alert(2)</script><image href="https://evil/x.png"/><a href="javascript:alert(3)">x</a><circle r="1"/></svg>');
  assert.ok(!/script/i.test(doc.replace(/Content-Security-Policy/, "")));
  assert.ok(!/onload/i.test(doc));
  assert.ok(!/evil|javascript:/i.test(doc));
  assert.ok(doc.includes("<circle r=\"1\"/>"));
  assert.ok(doc.includes("default-src 'none'"));
});
