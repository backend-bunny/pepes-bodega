<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { api, rawUrl, formatBytes, type MediaDetail } from '#lib/api.ts';
	import MediaCard from '#lib/MediaCard.svelte';

	let item = $state<MediaDetail | null>(null);
	let error = $state('');
	let video = $state<HTMLVideoElement>();

	const id = $derived(Number(page.params.id));
	const startAt = $derived(Number(page.url.searchParams.get('t') ?? 0));

	$effect(() => {
		item = null;
		error = '';
		api.get(id).then((d) => (item = d)).catch((e) => (error = String(e)));
	});

	$effect(() => {
		if (video && startAt) video.currentTime = startAt;
	});

	function seek(t: number) {
		if (video) {
			video.currentTime = t;
			video.play();
		}
	}

	async function remove() {
		if (!item || !confirm(`Remove "${item.original_filename}" from your library?`)) return;
		await api.remove(item.id);
		goto('/');
	}

	const ts = (s: number) => `${Math.floor(s / 60)}:${String(Math.floor(s % 60)).padStart(2, '0')}`;
	const ocr = $derived(item ? [...new Set(item.frames.map((f) => f.ocr_text).filter(Boolean))] : []);
</script>

<main>
	{#if error}
		<p class="error">{error}</p>
	{:else if !item}
		<p class="muted">Loading…</p>
	{:else}
		<div class="layout">
			<div class="viewer">
				{#if item.kind === 'video'}
					<!-- svelte-ignore a11y_media_has_caption -->
					<video bind:this={video} src={rawUrl(item.sha256)} controls loop></video>
				{:else}
					<img src={rawUrl(item.sha256)} alt={item.original_filename} />
				{/if}
			</div>
			<aside>
				<h2>{item.original_filename}</h2>
				<p class="muted">
					{item.kind} · {item.mime_type} · {formatBytes(item.size_bytes)}
					{#if item.width}· {item.width}×{item.height}{/if}
					{#if item.duration_s}· {item.duration_s.toFixed(1)}s{/if}
					<br />uploaded {new Date(item.uploaded_at).toLocaleString()}
					{#if item.source_device}from {item.source_device}{/if}
				</p>
				<p>
					<a href={rawUrl(item.sha256)} download={item.original_filename}><button>Download</button></a>
					<button class="danger" onclick={remove}>Remove</button>
				</p>

				{#if item.status !== 'ready'}
					<p class="pill">status: {item.status}</p>
					{#if item.error}<pre class="error">{item.error}</pre>{/if}
				{/if}

				{#if item.tags.length}
					<h3>Tags</h3>
					<div>
						{#each item.tags as t (t.tag + t.source)}
							<a class="pill" href={`/?q=${encodeURIComponent(t.tag)}`} title={`${t.source} ${(t.score * 100).toFixed(0)}%`}>{t.tag}</a>
						{/each}
					</div>
				{/if}

				{#if ocr.length}
					<h3>Text in image</h3>
					{#each ocr as line (line)}<p class="quote">{line}</p>{/each}
				{/if}

				{#if item.transcript}
					<h3>Transcript</h3>
					<p class="quote">{item.transcript}</p>
				{/if}

				{#if item.kind === 'video' && item.frames.length > 1}
					<h3>Keyframes</h3>
					<div>
						{#each item.frames as f (f.idx)}
							<button class="pill" onclick={() => seek(f.ts_s)}>{ts(f.ts_s)}</button>
						{/each}
					</div>
				{/if}

				<p class="muted sha">sha256 {item.sha256}</p>
			</aside>
		</div>

		{#if item.duplicates?.length}
			<h3>Near-duplicates in your library</h3>
			<div class="grid">
				{#each item.duplicates as d (d.id)}<MediaCard item={d} />{/each}
			</div>
		{/if}
	{/if}
</main>

<style>
	.layout {
		display: grid;
		grid-template-columns: minmax(0, 2fr) minmax(280px, 1fr);
		gap: 1.5rem;
	}
	@media (max-width: 800px) {
		.layout {
			grid-template-columns: 1fr;
		}
	}
	.viewer {
		background: #000;
		border-radius: var(--radius);
		display: grid;
		place-items: center;
		min-height: 300px;
	}
	.viewer img,
	.viewer video {
		max-width: 100%;
		max-height: 80vh;
	}
	h2 {
		word-break: break-word;
		margin-top: 0;
	}
	.quote {
		background: var(--panel);
		padding: 0.5rem 0.7rem;
		border-left: 3px solid var(--accent-2);
		border-radius: 4px;
		white-space: pre-wrap;
	}
	.sha {
		font-size: 0.75rem;
		word-break: break-all;
	}
	.grid {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(150px, 1fr));
		gap: 0.6rem;
	}
	.error {
		color: var(--danger);
		white-space: pre-wrap;
	}
</style>
