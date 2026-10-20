<script lang="ts">
	import { rawUrl, thumbUrl, type MediaItem } from './api.ts';
	import { formatDuration, snippetParts } from './format.ts';

	let {
		item,
		width,
		height,
		onopen
	}: {
		item: MediaItem;
		width: number;
		height: number;
		onopen?: (item: MediaItem, e: MouseEvent) => void;
	} = $props();

	// Hover plays GIFs/videos: the original file is only loaded while hovered.
	let hovering = $state(false);
	const playable = $derived(item.status === 'ready' && item.kind !== 'image');

	const badge = $derived(
		item.kind === 'animation' ? 'GIF' : item.kind === 'video' ? formatDuration(item.duration_s ?? 0) : null
	);
	const sourceLabel: Record<string, string> = { text: 'In image', speech: 'Said' };
	const snippet = $derived(
		item.match_source && item.match_snippet && sourceLabel[item.match_source]
			? { label: sourceLabel[item.match_source], parts: snippetParts(item.match_snippet) }
			: null
	);

	function click(e: MouseEvent) {
		// Let modified clicks open a new tab with the standalone page.
		if (!onopen || e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return;
		e.preventDefault();
		onopen(item, e);
	}
</script>

<a
	class="card"
	href={`/m/${item.id}`}
	title={item.original_filename}
	style:width="{width}px"
	style:height="{height}px"
	onclick={click}
	onpointerenter={(e) => e.pointerType === 'mouse' && (hovering = true)}
	onpointerleave={() => (hovering = false)}
	data-id={item.id}
>
	{#if item.status === 'ready'}
		<img src={thumbUrl(item.sha256)} alt="" loading="lazy" decoding="async" />
		{#if hovering && playable}
			{#if item.kind === 'video'}
				<video class="live" src={rawUrl(item.sha256)} muted loop playsinline autoplay></video>
			{:else}
				<img class="live" src={rawUrl(item.sha256)} alt="" />
			{/if}
		{/if}
	{:else}
		<div class="placeholder mono">{item.status === 'failed' ? 'failed' : item.status}</div>
	{/if}

	{#if badge}<span class="badge mono">{badge}</span>{/if}

	{#if snippet}
		<div class="snippet">
			<span class="source">{snippet.label}</span>
			<span class="text">
				{#each snippet.parts as p, i (i)}{#if p.hit}<mark>{p.text}</mark>{:else}{p.text}{/if}{/each}
			</span>
		</div>
	{/if}
</a>

<style>
	.card {
		position: relative;
		display: block;
		flex: none;
		overflow: hidden;
		border-radius: var(--r-sm);
		background: var(--c-surface);
	}
	img,
	video {
		display: block;
		width: 100%;
		height: 100%;
		object-fit: cover;
	}
	.live {
		position: absolute;
		inset: 0;
	}
	.card:hover img:not(.live) {
		filter: brightness(1.06);
	}
	.placeholder {
		display: grid;
		place-items: center;
		height: 100%;
		color: var(--c-subtle);
		font-size: var(--text-xs);
	}
	.badge {
		position: absolute;
		top: var(--sp-1);
		right: var(--sp-1);
		padding: 1px var(--sp-1);
		border-radius: var(--r-sm);
		background: rgb(0 0 0 / 0.55);
		color: var(--c-text);
		font-size: var(--text-2xs);
		line-height: 1.4;
	}
	.snippet {
		position: absolute;
		inset: auto 0 0 0;
		padding: var(--sp-5) var(--sp-2) var(--sp-2);
		background: var(--c-scrim);
		font-size: var(--text-xs);
		line-height: 1.35;
	}
	.source {
		display: block;
		color: var(--c-muted);
		font-size: var(--text-2xs);
		text-transform: uppercase;
		letter-spacing: 0.06em;
	}
	.text {
		display: -webkit-box;
		-webkit-line-clamp: 2;
		line-clamp: 2;
		-webkit-box-orient: vertical;
		overflow: hidden;
	}
	mark {
		background: none;
		color: var(--c-accent);
		font-weight: 600;
	}
</style>
