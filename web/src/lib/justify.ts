// Justified rows: every full row exactly fills the container width at the
// items' aspect ratios; the last row keeps the target height (not stretched).

export interface Sized {
	width: number | null;
	height: number | null;
}

export interface Row<T> {
	height: number;
	items: { item: T; width: number }[];
}

/** Aspect ratio for layout; unknown (still processing) is square, extremes are clamped. */
export function aspect(s: Sized): number {
	if (!s.width || !s.height) return 1;
	return Math.min(Math.max(s.width / s.height, 0.5), 2.5);
}

export function justify<T extends Sized>(items: T[], containerWidth: number, target: number, gap: number): Row<T>[] {
	if (containerWidth <= 0) return [];
	const rows: Row<T>[] = [];
	let cur: { item: T; a: number }[] = [];
	let sum = 0;
	const close = (height: number) => {
		rows.push({ height, items: cur.map(({ item, a }) => ({ item, width: a * height })) });
		cur = [];
		sum = 0;
	};
	for (const item of items) {
		const a = aspect(item);
		cur.push({ item, a });
		sum += a;
		const gaps = gap * (cur.length - 1);
		if (sum * target + gaps >= containerWidth) close((containerWidth - gaps) / sum);
	}
	if (cur.length) close(target);
	return rows;
}
