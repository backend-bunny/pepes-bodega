// Display helpers shared by all pages.

export function formatBytes(n: number): string {
	const units = ['B', 'KB', 'MB', 'GB'];
	let i = 0;
	while (n >= 1024 && i < units.length - 1) {
		n /= 1024;
		i++;
	}
	return `${n.toFixed(i ? 1 : 0)} ${units[i]}`;
}

export function formatDuration(s: number): string {
	const m = Math.floor(s / 60);
	return `${m}:${String(Math.floor(s % 60)).padStart(2, '0')}`;
}

/** 2026-10-20 14:03 (local time), for mono timestamps. */
export function isoMinute(iso: string): string {
	const d = new Date(iso);
	const p = (n: number) => String(n).padStart(2, '0');
	return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
}

const rtf = new Intl.RelativeTimeFormat('en', { numeric: 'auto' });
/** "3 days ago", falling back to the ISO date after a month. */
export function relativeTime(iso: string, now = Date.now()): string {
	const sec = (new Date(iso).getTime() - now) / 1000;
	const abs = Math.abs(sec);
	if (abs < 60) return 'just now';
	if (abs < 3600) return rtf.format(Math.round(sec / 60), 'minute');
	if (abs < 86400) return rtf.format(Math.round(sec / 3600), 'hour');
	if (abs < 30 * 86400) return rtf.format(Math.round(sec / 86400), 'day');
	return isoMinute(iso).slice(0, 10);
}

/** First 8 and last 4 hex chars: 3f2a9c1e…b07d */
export function shortHash(sha: string): string {
	return `${sha.slice(0, 8)}…${sha.slice(-4)}`;
}

/** CLIP prompt labels read like "a cat" / "an old man" / "a photograph"; drop the article. */
export function cleanTag(tag: string): string {
	return tag.replace(/^(a|an|the)\s+/i, '');
}

/** Split an API snippet into plain and highlighted parts (⟦…⟧ marks matched words). */
export function snippetParts(s: string): { text: string; hit: boolean }[] {
	return s
		.split(/(⟦[^⟧]*⟧)/)
		.filter(Boolean)
		.map((p) => (p.startsWith('⟦') ? { text: p.slice(1, -1), hit: true } : { text: p, hit: false }));
}

export async function copyText(text: string): Promise<boolean> {
	try {
		await navigator.clipboard.writeText(text);
		return true;
	} catch {
		// Non-secure contexts (plain http on a LAN IP) have no clipboard API.
		const ta = document.createElement('textarea');
		ta.value = text;
		ta.style.position = 'fixed';
		ta.style.opacity = '0';
		document.body.append(ta);
		ta.select();
		const ok = document.execCommand('copy');
		ta.remove();
		return ok;
	}
}
