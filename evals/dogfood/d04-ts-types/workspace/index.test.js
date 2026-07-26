import { test } from "node:test";
import assert from "node:assert";
import { groupBy } from "./index.js";

test("groups by key", () => {
  assert.deepStrictEqual(groupBy([1, 2, 3], (n) => (n % 2 ? "odd" : "even")), {
    odd: [1, 3],
    even: [2],
  });
});

test("handles empty input", () => {
  assert.deepStrictEqual(groupBy([], (n) => n), {});
});

test("keeps items with undefined key under 'undefined'", () => {
  assert.deepStrictEqual(groupBy([1], () => undefined), { undefined: [1] });
});
