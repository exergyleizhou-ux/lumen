export function groupBy(items, keyFn) {
  // BUG: crashes on empty input and drops items whose key is undefined.
  const out = {};
  for (const item of items) {
    const k = keyFn(item);
    out[k].push(item);
  }
  return out;
}
