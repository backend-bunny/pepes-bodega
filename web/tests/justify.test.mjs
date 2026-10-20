// node --test web/tests/justify.test.mjs  (plain node, no framework)
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { register } from 'node:module';

register('data:text/javascript,' + encodeURIComponent(`
export async function load(url, ctx, next) {
  if (url.endsWith('.ts')) {
    const { readFile } = await import('node:fs/promises');
    const src = await readFile(new URL(url), 'utf8');
    const { stripTypeScriptTypes } = await import('node:module');
    return { format: 'module', source: stripTypeScriptTypes(src), shortCircuit: true };
  }
  return next(url, ctx);
}`));
const { justify, aspect } = await import('../src/lib/justify.ts');

const img = (w, h) => ({ width: w, height: h });

test('full rows fill the container exactly', () => {
	const items = [img(400, 300), img(300, 300), img(600, 300), img(200, 400), img(500, 250), img(300, 300)];
	const rows = justify(items, 1000, 200, 6);
	for (const row of rows.slice(0, -1)) {
		const total = row.items.reduce((s, i) => s + i.width, 0) + 6 * (row.items.length - 1);
		assert.ok(Math.abs(total - 1000) < 0.01, `row width ${total}`);
	}
	assert.equal(rows.flatMap((r) => r.items).length, items.length);
});

test('last row keeps the target height', () => {
	const rows = justify([img(100, 100)], 1000, 200, 6);
	assert.equal(rows.length, 1);
	assert.equal(rows[0].height, 200);
});

test('unknown dimensions are square; extremes clamped', () => {
	assert.equal(aspect({ width: null, height: null }), 1);
	assert.equal(aspect(img(5000, 100)), 2.5);
	assert.equal(aspect(img(100, 5000)), 0.5);
});
