<script lang="ts">
	import type { MediaItem } from './api.ts';
	import { justify } from './justify.ts';
	import MediaCard from './MediaCard.svelte';

	let {
		items,
		onopen
	}: {
		items: MediaItem[];
		onopen?: (item: MediaItem, e: MouseEvent) => void;
	} = $props();

	const GAP = 6;
	let width = $state(0);
	// Shorter rows on phones so more than one item fits per row.
	const target = $derived(width < 600 ? 120 : 210);
	const rows = $derived(justify(items, width, target, GAP));
</script>

<div class="justified" bind:clientWidth={width} style:--gap="{GAP}px">
	{#each rows as row, r (r)}
		<div class="row" style:height="{row.height}px">
			{#each row.items as { item, width: w } (item.id)}
				<MediaCard {item} width={w} height={row.height} {onopen} />
			{/each}
		</div>
	{/each}
</div>

<style>
	.justified {
		display: grid;
		gap: var(--gap);
	}
	.row {
		display: flex;
		gap: var(--gap);
		overflow: hidden;
	}
</style>
