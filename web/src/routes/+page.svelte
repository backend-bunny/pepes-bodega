<script lang="ts">
	import { onMount, untrack } from 'svelte';
	import { goto, pushState, replaceState } from '$app/navigation';
	import { page } from '$app/state';
	import { api, subscribeProgress, type MediaItem, type Stats } from '#lib/api.ts';
	import JustifiedGrid from '#lib/JustifiedGrid.svelte';
	import Lightbox from '#lib/Lightbox.svelte';

	type Filter = 'all' | 'direct' | 'related';

	let query = $state(page.url.searchParams.get('q') ?? '');
	let applied = $state<string | null>(null); // query the current results belong to
	let items = $state<MediaItem[]>([]);
	let stats = $state<Stats | null>(null);
	let loading = $state(false);
	let done = $state(false);
	let semantic = $state(true);
	let error = $state('');
	let filter = $state<Filter>('all');
	let sentinel: HTMLDivElement;

	const searching = $derived((applied ?? '') !== '');
	// Search results come sorted: direct (text) matches first, then related (CLIP-only).
	const direct = $derived(items.filter((i) => i.match === 'direct'));
	const related = $derived(items.filter((i) => i.match === 'related'));
	const visible = $derived(
		!searching || filter === 'all' ? items : filter === 'direct' ? direct : related
	);

	async function run(q: string) {
		items = [];
		done = false;
		error = '';
		filter = 'all';
		if (q) {
			loading = true;
			try {
				const res = await api.search(q);
				if (applied === q) {
					items = res.items;
					semantic = res.semantic;
				}
			} catch (e) {
				error = String(e);
			} finally {
				loading = false;
				done = true;
			}
		} else {
			await more();
		}
	}

	async function more() {
		if (loading || done || searching) return;
		loading = true;
		try {
			const before = items.at(-1)?.id ?? 0;
			const res = await api.list(before, 60);
			items = [...items, ...res.items];
			done = res.items.length < 60;
		} catch (e) {
			error = String(e);
		} finally {
			loading = false;
		}
	}

	function submit(e: SubmitEvent) {
		e.preventDefault();
		const q = query.trim();
		goto(q ? `/?q=${encodeURIComponent(q)}` : '/', { reset: false });
	}

	// Load results for "/?q=…". The lightbox shallow-routes to /m/<id>, which must
	// not reload the results underneath it, so other paths are ignored.
	$effect(() => {
		if (page.url.pathname !== '/') return;
		const q = (page.url.searchParams.get('q') ?? '').trim();
		untrack(() => {
			if (q === applied) return;
			applied = q;
			query = q;
			run(q);
		});
	});

	// ---- lightbox (shallow routing) ----
	const openId = $derived(page.state.lightbox);
	const openIndex = $derived(openId == null ? -1 : visible.findIndex((i) => i.id === openId));

	function open(item: MediaItem) {
		pushState(`/m/${item.id}`, { lightbox: item.id });
	}
	function show(item: MediaItem) {
		replaceState(`/m/${item.id}`, { lightbox: item.id });
	}
	function close() {
		history.back();
	}
	function removed(id: number) {
		items = items.filter((i) => i.id !== id);
		close();
	}

	onMount(() => {
		// Initial stats; live updates arrive over SSE as workers process items.
		api.stats().then((s) => (stats = s)).catch(() => {});
		const stopProgress = subscribeProgress((ev) => {
			stats = ev.stats;
			for (const item of items) {
				const status = ev.blobs[item.sha256];
				if (status) item.status = status;
			}
		});
		const io = new IntersectionObserver((e) => e[0].isIntersecting && more(), { rootMargin: '800px' });
		io.observe(sentinel);
		return () => {
			io.disconnect();
			stopProgress();
		};
	});

	const chips: [Filter, string][] = [
		['all', 'All'],
		['direct', 'Matches'],
		['related', 'Vaguely related']
	];
	const counts = $derived({ all: items.length, direct: direct.length, related: related.length });
</script>

<main class="page">
	<form onsubmit={submit} role="search">
		<!-- svelte-ignore a11y_autofocus -->
		<input
			type="search"
			bind:value={query}
			placeholder="Search memes: “surprised cat”, “this is fine”, text in the image, words said in a clip…"
			aria-label="Search"
			autofocus
		/>
		<button class="btn btn-primary">Search</button>
	</form>

	{#if searching && done && items.length}
		<div class="chips" role="radiogroup" aria-label="Match type">
			{#each chips as [key, label] (key)}
				<button
					class="chip"
					role="radio"
					aria-checked={filter === key}
					disabled={counts[key] === 0}
					onclick={() => (filter = key)}
				>
					{label} <span class="mono">{counts[key]}</span>
				</button>
			{/each}
		</div>
	{/if}

	<p class="meta muted">
		{#if searching}
			<span class="mono">{direct.length}</span>
			{direct.length === 1 ? 'match' : 'matches'} · {semantic
				? `${related.length} vaguely related`
				: 'semantic search unavailable, text matches only'}
		{:else if stats}
			<span class="mono">{stats.media}</span> items · <span class="mono">{stats.by_status.ready ?? 0}</span> ready
			· <span class="mono">{(stats.by_status.pending ?? 0) + (stats.by_status.processing ?? 0)}</span> in the
			kitchen · <span class="mono">{stats.near_duplicates}</span> near-duplicates
		{/if}
	</p>

	{#if error}<p class="error">{error}</p>{/if}

	{#if searching}
		{#if filter !== 'related'}
			<JustifiedGrid items={direct} onopen={open} />
		{/if}
		{#if filter !== 'direct' && related.length}
			{#if filter === 'all'}
				<div class="divider" role="separator">
					<span>{direct.length ? 'Vaguely related' : 'No direct matches. Vaguely related'}</span>
					<small class="muted">similar look or meaning, but no tag or text match</small>
				</div>
			{/if}
			<JustifiedGrid items={related} onopen={open} />
		{/if}
	{:else}
		<JustifiedGrid {items} onopen={open} />
	{/if}

	{#if !loading && done && items.length === 0}
		<p class="muted empty">
			{searching ? 'Nothing on the shelves for that.' : 'The bodega is empty. Upload something!'}
		</p>
	{/if}
	{#if loading}<p class="muted">Loading…</p>{/if}
	<div bind:this={sentinel}></div>
</main>

{#if openId != null}
	<Lightbox
		id={openId}
		startAt={visible[openIndex]?.match_ts ?? 0}
		onclose={close}
		onprev={openIndex > 0 ? () => show(visible[openIndex - 1]) : undefined}
		onnext={openIndex >= 0 && openIndex < visible.length - 1 ? () => show(visible[openIndex + 1]) : undefined}
		onnavigate={show}
		onremoved={removed}
	/>
{/if}

<style>
	form {
		display: flex;
		gap: var(--sp-2);
	}
	form input {
		font-size: var(--text-lg);
		padding: var(--sp-3) var(--sp-4);
	}
	.chips {
		display: flex;
		flex-wrap: wrap;
		gap: var(--sp-2);
		margin-top: var(--sp-3);
	}
	.chip {
		display: inline-flex;
		gap: var(--sp-2);
		align-items: baseline;
		padding: var(--sp-1) var(--sp-3);
		border: 1px solid var(--c-border);
		border-radius: var(--r-pill);
		background: none;
		color: var(--c-muted);
		font-size: var(--text-sm);
		cursor: pointer;
	}
	.chip .mono {
		color: var(--c-subtle);
		font-size: var(--text-xs);
	}
	.chip[aria-checked='true'] {
		border-color: var(--c-accent);
		background: var(--c-accent-soft);
		color: var(--c-text);
	}
	.chip:disabled {
		opacity: 0.4;
		cursor: default;
	}
	.meta {
		margin: var(--sp-3) 0 var(--sp-4);
		font-size: var(--text-sm);
	}
	.meta .mono {
		color: var(--c-text);
	}
	.divider {
		display: flex;
		flex-wrap: wrap;
		align-items: baseline;
		gap: var(--sp-3);
		margin: var(--sp-6) 0 var(--sp-3);
		padding-top: var(--sp-3);
		border-top: 1px solid var(--c-border);
	}
	.divider span {
		font-family: var(--font-display);
		font-weight: 600;
	}
	.error {
		color: var(--c-danger);
	}
	.empty {
		padding: var(--sp-8);
		text-align: center;
	}
	@media (max-width: 600px) {
		form .btn {
			display: none;
		}
	}
</style>
