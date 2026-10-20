// Regression check: tall media must fit inside the viewer, never overlap the
// sections below it. Usage: [SESSION=…] node web/tests/tall-image.mjs SERVER ID [OUT_PREFIX]
// (SESSION is a bodega_session cookie; omit it against a dev-auth API.)
import { chromium } from 'playwright-core';
import { execSync } from 'node:child_process';

const [server, id, out] = process.argv.slice(2);
const executablePath = process.env.CHROMIUM ?? execSync('command -v chromium').toString().trim();
const browser = await chromium.launch({ executablePath, args: ['--no-sandbox'] });
let failed = false;
const viewports = [
	['desktop', { width: 1440, height: 900 }],
	['phone', { width: 390, height: 844 }]
];
for (const [name, viewport] of viewports) {
	const ctx = await browser.newContext({ viewport });
	if (process.env.SESSION) await ctx.addCookies([
		{ name: 'bodega_session', value: process.env.SESSION, domain: new URL(server).hostname, path: '/' }
	]);
	const page = await ctx.newPage();
	await page.goto(`${server}/m/${id}`);
	const media = page.locator('.stage img, .stage video').first();
	await media.waitFor();
	await page.waitForFunction(() => {
		const i = document.querySelector('.stage img');
		return !i || i.complete;
	});
	await page.locator('.below').first().waitFor({ timeout: 10000 }).catch(() => {});
	const m = await media.boundingBox();
	const s = await page.locator('.stage').boundingBox();
	// No section below yet for unprocessed items (no embedding, no duplicates).
	const below = (await page.locator('.below').count()) ? await page.locator('.below').first().boundingBox() : null;
	const bottom = m.y + m.height;
	const ok = m.y >= s.y - 1 && bottom <= s.y + s.height + 1 && (!below || bottom <= below.y + 1);
	console.log(
		`${name}: media ${Math.round(m.width)}x${Math.round(m.height)} y=${Math.round(m.y)}..${Math.round(bottom)},` +
			` stage ends ${Math.round(s.y + s.height)}, next section ${below ? Math.round(below.y) : '-'} -> ${ok ? 'OK' : 'OVERLAP'}`
	);
	if (!ok) failed = true;
	if (out) await page.screenshot({ path: `${out}-${name}.png` });
	await ctx.close();
}
await browser.close();
process.exit(failed ? 1 : 0);
