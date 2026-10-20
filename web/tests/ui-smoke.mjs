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
		['/settings', 'h1', /Device tokens/],
		['/upload', '.drop', /Drop memes here/],
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
		const firstGrid = page.locator('main .justified').first();
		if ((await firstGrid.locator('a.card[title="200% bother.gif"]').count()) !== 1)
			fail('direct match not in the first grid');
	} catch {
		fail('search page has no direct/related separator');
	}

	// The direct text hit says where it matched, with the query highlighted.
	const snippet = page.locator('a.card[title="200% bother.gif"] .snippet');
	if (!(await snippet.count())) fail('text hit has no match-source snippet');
	else if (!/bother/i.test((await snippet.locator('mark').first().textContent()) ?? ''))
		fail('snippet does not highlight the query');

	// Filter chips: "Matches" hides the related group.
	await page.locator('.chip', { hasText: 'Matches' }).click();
	if (await page.locator('.divider').count()) fail('Matches filter still shows the related group');
	await page.locator('.chip', { hasText: 'All' }).click();

	// Lightbox: shallow route to /m/<id>, arrow keys navigate, Esc returns to the results.
	await page.locator('a.card').first().click();
	const title = page.locator('[role=dialog] h2');
	await title.filter({ hasText: /\S/ }).waitFor({ timeout: 10000 });
	if (!/\/m\/\d+$/.test(page.url())) fail(`lightbox URL is ${page.url()}`);
	const firstTitle = await title.textContent();
	await page.keyboard.press('ArrowRight');
	await page
		.waitForFunction((t) => document.querySelector('[role=dialog] h2')?.textContent !== t, firstTitle, {
			timeout: 5000
		})
		.catch(() => fail('ArrowRight did not move to the next item'));
	await page.keyboard.press('Escape');
	await page.waitForURL(/\?q=bother/, { timeout: 5000 }).catch(() => fail(`Esc did not return: ${page.url()}`));
	if (await page.locator('[role=dialog]').count()) fail('lightbox still open after Esc');

	// Clicking the media keeps the viewer open; clicking the backdrop around it closes.
	await page.locator('a.card').first().click();
	await title.filter({ hasText: /\S/ }).waitFor({ timeout: 10000 });
	await page.locator('.stage img, .stage video').first().click();
	await page.waitForTimeout(300);
	if (!(await page.locator('[role=dialog]').count())) fail('clicking the image closed the viewer');
	const stage = await page.locator('.stage').boundingBox();
	await page.mouse.click(stage.x + 6, stage.y + 6);
	await page.waitForURL(/\?q=bother/, { timeout: 5000 }).catch(() => fail(`backdrop click did not return: ${page.url()}`));
	if (await page.locator('[role=dialog]').count()) fail('lightbox still open after backdrop click');

	// Standalone /m/<id> (reload / shared link) renders the same lightbox.
	const firstId = await (await page.request.get(`${server}/api/v1/media`)).json().then((b) => b.items[0].id);
	await page.goto(`${server}/m/${firstId}`);
	await page
		.locator('[role=dialog] h2')
		.filter({ hasText: /\S/ })
		.waitFor({ timeout: 10000 })
		.catch(() => fail('detail page did not render'));

	if (errors.length) fail(`page errors: ${errors.join(' | ')}`);
} finally {
	await browser.close();
}
if (!process.exitCode) console.log('UI OK');
