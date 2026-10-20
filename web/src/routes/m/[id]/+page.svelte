<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import type { MediaItem } from '#lib/api.ts';
	import Lightbox from '#lib/Lightbox.svelte';

	// Direct visit / reload of /m/<id>: the lightbox standalone, closing to search.
	const id = $derived(Number(page.params.id));
	const startAt = $derived(Number(page.url.searchParams.get('t') ?? 0));
</script>

<Lightbox
	{id}
	{startAt}
	onclose={() => goto('/')}
	onnavigate={(item: MediaItem) => goto(`/m/${item.id}`, { replace: true })}
	onremoved={() => goto('/')}
/>
