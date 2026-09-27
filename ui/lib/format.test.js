// Unit tests for the pure body-formatting helpers. Run with `npm run test:js`
// (node --test), so they need no bundler, DOM, or browser.
import test from "node:test";
import assert from "node:assert/strict";

import { collapseRuns } from "./format.js";

// Rebuilding the original from the segments proves the transform never drops or
// duplicates characters; every case below asserts it.
function rejoin(segments) {
  return segments.map((s) => s.text).join("");
}

test("collapseRuns leaves a short body as one plain segment", () => {
  const body = `{"hello":"world"}`;
  const out = collapseRuns(body);
  assert.equal(out.length, 1);
  assert.equal(out[0].blob, false);
  assert.equal(rejoin(out), body);
});

test("collapseRuns is a no-op for an empty body", () => {
  const out = collapseRuns("");
  assert.equal(out.length, 1);
  assert.equal(out[0].blob, false);
  assert.equal(rejoin(out), "");
});

test("collapseRuns marks a large blob and preserves the surrounding text", () => {
  const blob = "A".repeat(8192); // 8 KiB, well over the 2 KiB threshold
  const body = `{"kind":"large","blob":"${blob}"}`;
  const out = collapseRuns(body);

  assert.equal(out.length, 3);
  assert.equal(out[0].text, `{"kind":"large","blob":"`);
  assert.equal(out[1].blob, true);
  assert.equal(out[1].bytes, blob.length);
  assert.equal(out[1].text, blob);
  assert.equal(out[2].text, `"}`);
  assert.equal(rejoin(out), body);
});

test("collapseRuns labels the blob kind from its prefix", () => {
  const kindOf = (body) => collapseRuns(body).find((s) => s.blob).kind;

  assert.equal(kindOf(`{"b":"${"QUJD".repeat(2000)}"}`), "base64");
  assert.equal(kindOf(`{"b":"${"deadbeef".repeat(1000)}"}`), "hex");
  assert.equal(kindOf(`{"b":"${"iVBORw0KGgo".padEnd(8192, "A")}"}`), "image/png");
  assert.equal(kindOf(`src=${"data:image/png;base64,".padEnd(4096, "A")}`), "data URI");
});

test("collapseRuns keeps runs below the threshold inline", () => {
  const small = "A".repeat(1024);
  const body = `{"b":"${small}"}`;
  const out = collapseRuns(body);
  assert.equal(out.length, 1);
  assert.equal(out[0].blob, false);
  assert.equal(rejoin(out), body);
});

test("collapseRuns handles a multi-megabyte run without splitting it", () => {
  const blob = "A".repeat(3 * 1024 * 1024);
  const out = collapseRuns(blob);
  assert.equal(out.length, 1);
  assert.equal(out[0].blob, true);
  assert.equal(out[0].bytes, blob.length);
});

test("collapseRuns collapses each of several large runs", () => {
  const one = "A".repeat(4096);
  const two = "B".repeat(4096);
  const body = `{"a":"${one}","b":"${two}"}`;
  const out = collapseRuns(body);
  assert.equal(out.filter((s) => s.blob).length, 2);
  assert.equal(rejoin(out), body);
});
