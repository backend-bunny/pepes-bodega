<script lang="ts">
	import { thumbUrl, type MediaItem } from './api.ts';

	let { item }: { item: MediaItem } = $props();

	const ts = (s: number) => `${Math.floor(s / 60)}:${String(Math.floor(s % 60)).padStart(2, '0')}`;
</script>

<a class="card" href={`/m/${item.id}${item.match_ts != null ? `?t=${item.match_ts}` : ''}`} title={item.original_filename}>
	{#if item.status === 'ready'}
		<img src={thumbUrl(item.sha256)} alt={item.original_filename} loading="lazy" />
	{:else}
		<div class="placeholder">{item.status === 'failed' ? '💥 failed' : '⏳ ' + item.status}</div>
	{/if}
	<div class="badges">
		{#if item.kind !== 'image'}<span class="pill">{item.kind === 'video' ? '▶' : 'GIF'}</span>{/if}
		{#if item.match_ts != null}<span class="pill">@ {ts(item.match_ts)}</span>{/if}
		{#if item.duplicate_of}<span class="pill" title="near-duplicate of another item">≈ dup</span>{/if}
	</div>
</a>

<style>
	.card {
		position: relative;
		display: block;
		aspect-ratio: 1;
		background: var(--panel);
		border-radius: var(--radius);
		overflow: hidden;
		border: 1px solid transparent;
	}
	.card:hover {
		border-color: var(--accent);
	}
	img {
		width: 100%;
		height: 100%;
		object-fit: cover;
	}
	.placeholder {
		display: grid;
		place-items: center;
		height: 100%;
		color: var(--muted);
	}
	.badges {
		position: absolute;
		left: 0.4rem;
		bottom: 0.3rem;
	}
	.badges .pill {
		background: #000a;
	}
</style>
