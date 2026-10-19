<script lang="ts">
	import { onMount, untrack } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { api, type MediaItem, type Stats } from '#lib/api.ts';
	import MediaCard from '#lib/MediaCard.svelte';

	let query = $state(page.url.searchParams.get('q') ?? '');
	let items = $state<MediaItem[]>([]);
	let stats = $state<Stats | null>(null);
	let loading = $state(false);
	let done = $state(false);
	let semantic = $state(true);
	let error = $state('');
	let sentinel: HTMLDivElement;

	const searching = $derived((page.url.searchParams.get('q') ?? '').trim() !== '');

	async function run() {
		const q = (page.url.searchParams.get('q') ?? '').trim();
		items = [];
		done = false;
		error = '';
		if (q) {
			loading = true;
			try {
				const res = await api.search(q);
				items = res.items;
				semantic = res.semantic;
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
		goto(q ? `/?q=${encodeURIComponent(q)}` : '/');
	}

	// Depend only on the query string. run() reads and writes items/loading/done;
	// if those were tracked, every fetch would re-trigger the effect (infinite loop).
	$effect(() => {
		page.url.search;
		untrack(run);
	});

	onMount(() => {
		api.stats().then((s) => (stats = s)).catch(() => {});
		const io = new IntersectionObserver((e) => e[0].isIntersecting && more(), { rootMargin: '600px' });
		io.observe(sentinel);
		return () => io.disconnect();
	});
</script>

<main>
	<form onsubmit={submit}>
		<!-- svelte-ignore a11y_autofocus -->
		<input
			type="search"
			bind:value={query}
			placeholder="Search memes: “surprised cat”, “this is fine”, text in the image, words said in a clip…"
			autofocus
		/>
		<button class="primary">Search</button>
	</form>

	<p class="muted meta">
		{#if searching}
			{items.length} results{#if !semantic} · semantic search unavailable, text matches only{/if}
		{:else if stats}
			{stats.media} items · {stats.by_status.ready ?? 0} ready · {(stats.by_status.pending ?? 0) +
				(stats.by_status.processing ?? 0)} in the kitchen · {stats.near_duplicates} near-duplicates
		{/if}
	</p>

	{#if error}<p class="error">{error}</p>{/if}

	<div class="grid">
		{#each items as item (item.id)}
			<MediaCard {item} />
		{/each}
	</div>

	{#if !loading && done && items.length === 0}
		<p class="muted empty">
			{searching ? 'Nothing on the shelves for that.' : 'The bodega is empty. Upload something!'}
		</p>
	{/if}
	{#if loading}<p class="muted">Loading…</p>{/if}
	<div bind:this={sentinel}></div>
</main>

<style>
	form {
		display: flex;
		gap: 0.5rem;
	}
	form input {
		flex: 1;
		font-size: 1.1rem;
	}
	.meta {
		margin: 0.5rem 0 1rem;
		font-size: 0.9rem;
	}
	.grid {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(170px, 1fr));
		gap: 0.6rem;
	}
	.error {
		color: var(--danger);
	}
	.empty {
		text-align: center;
		padding: 3rem;
	}
</style>
