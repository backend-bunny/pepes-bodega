// Screenshot every page at desktop and phone widths (dev tool, not a test).
//   SESSION=<bodega_session cookie> node web/tests/screenshots.mjs SERVER OUTDIR [UPLOAD_FILE]
import { chromium } from 'playwright-core';
import { execSync } from 'node:child_process';
import { mkdirSync } from 'node:fs';

const [server, out, uploadFile] = process.argv.slice(2);
const executablePath = process.env.CHROMIUM ?? execSync('command -v chromium').toString().trim();
mkdirSync(out, { recursive: true });
const host = new URL(server).hostname;

const viewports = [
	['desktop', { width: 1440, height: 900 }],
	['phone', { width: 390, height: 844 }]
];

const browser = await chromium.launch({ executablePath, args: ['--no-sandbox', '--disable-dev-shm-usage'] });
const problems = [];
try {
	for (const [vname, viewport] of viewports) {
		const ctx = await browser.newContext({ viewport, deviceScaleFactor: 1, colorScheme: 'dark' });
		await ctx.addCookies([{ name: 'bodega_session', value: process.env.SESSION, domain: host, path: '/' }]);
		const page = await ctx.newPage();
		page.on('pageerror', (e) => problems.push(`${vname} pageerror: ${e.message}`));
		page.on('crash', () => problems.push(`${vname}: renderer crashed`));
		page.on('console', (m) => m.type() === 'error' && problems.push(`${vname} console: ${m.text()}`));
		const shot = async (name, opts = {}) => {
			await page.waitForTimeout(600);
			await page.screenshot({ path: `${out}/${vname}-${name}.png`, ...opts });
		};
		// Not 'networkidle': the page keeps an SSE stream open, so it never goes idle.
		const settle = async () => {
			await page
				.waitForFunction(() => [...document.images].filter((i) => i.loading !== 'lazy' || i.getBoundingClientRect().top < innerHeight).every((i) => i.complete), null, { timeout: 15000 })
				.catch(() => problems.push(`${vname}: images still loading at ${page.url()}`));
		};

		await page.goto(`${server}/`);
		await page.locator('a.card').first().waitFor();
		await settle();
		await shot('01-browse');

		await page.goto(`${server}/?q=cat`);
		await page.locator('.divider').waitFor();
		await settle();
		await shot('02-search-cat');
		await page.locator('.divider').scrollIntoViewIfNeeded();
		await shot('03-search-cat-divider');

		await page.goto(`${server}/?q=weather`);
		await page.locator('a.card').first().waitFor();
		await settle();
		await page.locator('a.card').first().hover();
		await shot('04-search-text-hit');

		// Lightbox via shallow routing from the results.
		await page.goto(`${server}/?q=cat`);
		await page.locator('a.card').first().waitFor();
		await page.locator('a.card').first().click();
		await page.locator('[role=dialog] h2').filter({ hasText: /\S/ }).waitFor();
		await settle();
		if (!/\/m\/\d+$/.test(page.url())) problems.push(`${vname}: lightbox URL not /m/<id>: ${page.url()}`);
		await shot('05-lightbox');
		await page.keyboard.press('ArrowRight');
		await page.waitForTimeout(800);
		await settle();
		await shot('06-lightbox-next');
		const similar = page.locator('[role=dialog] .section-title', { hasText: 'Similar' });
		await similar.waitFor({ timeout: 10000 }).catch(() => problems.push(`${vname}: no Similar section`));
		await similar.scrollIntoViewIfNeeded().catch(() => {});
		await settle();
		await shot('07-lightbox-similar');
		await page.keyboard.press('Escape');
		await page.waitForTimeout(500);
		if (!page.url().includes('/?q=cat')) problems.push(`${vname}: Esc did not return to results: ${page.url()}`);

		const id = new URL(page.url()).searchParams.get('q') ? await page.locator('a.card').nth(2).getAttribute('data-id') : null;
		await page.goto(`${server}/m/${id}`);
		await page.locator('[role=dialog] h2').filter({ hasText: /\S/ }).waitFor();
		await settle();
		await shot('08-detail-standalone');

		await page.goto(`${server}/upload`);
		await page.locator('.drop').waitFor();
		await shot('09-upload-empty');
		if (uploadFile) {
			await page.locator('input[type=file]').setInputFiles(uploadFile);
			await page.locator('.steps').first().waitFor();
			await page.waitForTimeout(1500);
			await shot('10-upload-queue');
		}

		await page.goto(`${server}/settings`);
		await page.locator('h1', { hasText: 'Device tokens' }).waitFor();
		await shot('11-settings');
		if (vname === 'desktop') {
			const tokenName = `screenshot-${Date.now()}`;
			await page.locator('input[name=bodega-device-label]').fill(tokenName);
			await page.locator('form button').click();
			await page.locator('.once').waitFor();
			await shot('12-settings-token-created');
			// Clean up the token through the UI (also exercises the confirmation).
			const row = page.locator('.tokens li', { hasText: tokenName });
			try {
				await row.locator('button', { hasText: 'Revoke' }).click();
				await shot('13-settings-revoke-confirm');
				await row.locator('button.danger', { hasText: 'Revoke' }).click();
				await row.waitFor({ state: 'detached' });
			} finally {
				// Never leave a live token behind, even if the UI path failed.
				const { items } = await (await page.request.get(`${server}/api/v1/tokens`)).json();
				for (const t of items.filter((t) => t.name === tokenName))
					await page.request.delete(`${server}/api/v1/tokens/${t.id}`);
			}
		}
		await ctx.close();
	}
} finally {
	await browser.close();
}
console.log(problems.length ? `PROBLEMS:\n${problems.join('\n')}` : 'no page errors');
