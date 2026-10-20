// UI smoke test against a running bodega-api (dev auth), driven by the
// system/devbox Chromium via playwright-core (no browser download).
//   node web/tests/ui-smoke.mjs SERVER EXPECTED_ITEMS DELETE_ID
// Checks every page renders, the home page doesn't refetch in a loop, and the
// stats line updates live (SSE) when an item is deleted, without a reload.
import { chromium } from 'playwright-core';
import { execSync } from 'node:child_process';

const [server, expectedItems, deleteId] = process.argv.slice(2);
if (!server || !expectedItems || !deleteId) {
	console.error('usage: ui-smoke.mjs SERVER EXPECTED_ITEMS DELETE_ID');
	process.exit(2);
}
const executablePath = process.env.CHROMIUM ?? execSync('command -v chromium').toString().trim();
const want = Number(expectedItems);

const fail = (msg) => {
	console.error(`FAIL: ${msg}`);
	process.exitCode = 1;
};

const browser = await chromium.launch({ executablePath, args: ['--no-sandbox'] });
try {
	const page = await browser.newPage();
	const errors = [];
	page.on('pageerror', (e) => errors.push(e.message));
	let listRequests = 0;
	page.on('request', (r) => {
		if (new URL(r.url()).pathname === '/api/v1/media' && r.method() === 'GET') listRequests++;
	});

	// Home: all cards, stats line, no fetch loop.
	await page.goto(`${server}/`);
	await page.locator('a.card').nth(want - 1).waitFor({ timeout: 15000 });
	const cards = await page.locator('a.card').count();
	if (cards !== want) fail(`home rendered ${cards} cards, want ${want}`);
	await page.waitForTimeout(2000);
	if (listRequests > 3) fail(`home made ${listRequests} list requests (fetch loop?)`);
	const stats = page.locator('p.meta');
	await stats.filter({ hasText: `${want} items` }).waitFor({ timeout: 5000 });

	// Live update: delete via the API with the page open; the line must follow.
	const res = await page.request.delete(`${server}/api/v1/media/${deleteId}`);
	if (res.status() !== 204) fail(`delete returned ${res.status()}`);
	try {
		await stats.filter({ hasText: `${want - 1} items` }).waitFor({ timeout: 5000 });
	} catch {
		fail(`stats line did not update live after delete: "${await stats.textContent()}"`);
	}

	// Other pages render their main content.
	const checks = [
		['/?q=bother', 'p.meta', /1 match · \d+ vaguely related/],
		['/settings', 'h2', /Device tokens/],
		['/upload', 'label.drop', /Drop memes here/],
	];
	for (const [path, selector, text] of checks) {
		await page.goto(`${server}${path}`);
		try {
			await page.locator(selector).filter({ hasText: text }).waitFor({ timeout: 10000 });
		} catch {
			const seen = await page.locator(selector).allTextContents().catch(() => []);
			fail(`${path}: no ${selector} matching ${text} (found: ${JSON.stringify(seen)})`);
		}
	}
	// Search: the text hit sits in the first grid, above the 'Vaguely related' separator.
	await page.goto(`${server}/?q=bother`);
	try {
		await page.locator('.divider').waitFor({ timeout: 10000 });
		const firstGrid = page.locator('main .grid').first();
		if ((await firstGrid.locator('a.card[title="200% bother.gif"]').count()) !== 1)
			fail('direct match not in the first grid');
	} catch {
		fail('search page has no direct/related separator');
	}

	const firstId = await (await page.request.get(`${server}/api/v1/media`)).json().then((b) => b.items[0].id);
	await page.goto(`${server}/m/${firstId}`);
	await page.locator('aside h2').waitFor({ timeout: 10000 }).catch(() => fail('detail page did not render'));

	if (errors.length) fail(`page errors: ${errors.join(' | ')}`);
} finally {
	await browser.close();
}
if (!process.exitCode) console.log('UI OK');
