<script lang="ts">
	import { onMount } from 'svelte';
	import { api, rawUrl, type MediaDetail, type MediaItem } from './api.ts';
	import { cleanTag, copyText, formatBytes, formatDuration, isoMinute, shortHash } from './format.ts';
	import JustifiedGrid from './JustifiedGrid.svelte';

	let {
		id,
		startAt = 0,
		onclose,
		onprev,
		onnext,
		onnavigate,
		onremoved
	}: {
		id: number;
		/** Seek videos here on open (best-matching keyframe of a search hit). */
		startAt?: number;
		onclose: () => void;
		onprev?: () => void;
		onnext?: () => void;
		/** Open another item (similar / near-duplicate) in place. */
		onnavigate: (item: MediaItem) => void;
		onremoved?: (id: number) => void;
	} = $props();

	let item = $state<MediaDetail | null>(null);
	let similar = $state<MediaItem[]>([]);
	let error = $state('');
	let confirming = $state(false);
	let copied = $state(false);
	let video = $state<HTMLVideoElement>();

	$effect(() => {
		const current = id;
		item = null;
		similar = [];
		error = '';
		confirming = false;
		api.get(current).then((d) => current === id && (item = d)).catch((e) => (error = String(e)));
		api.similar(current).then((r) => current === id && (similar = r.items)).catch(() => {});
	});

	$effect(() => {
		if (video && startAt) video.currentTime = startAt;
	});

	onMount(() => {
		const prevOverflow = document.body.style.overflow;
		document.body.style.overflow = 'hidden';
		return () => (document.body.style.overflow = prevOverflow);
	});

	function keydown(e: KeyboardEvent) {
		if (e.target instanceof HTMLInputElement) return;
		if (e.key === 'Escape') onclose();
		else if (e.key === 'ArrowLeft' && onprev) onprev();
		else if (e.key === 'ArrowRight' && onnext) onnext();
	}

	async function copyHash() {
		if (item && (await copyText(item.sha256))) {
			copied = true;
			setTimeout(() => (copied = false), 1500);
		}
	}

	async function remove() {
		if (!item) return;
		await api.remove(item.id);
		onremoved?.(item.id);
	}

	const ocr = $derived(item ? [...new Set(item.frames.map((f) => f.ocr_text).filter(Boolean))] : []);
	const tags = $derived(item ? [...new Set(item.tags.map((t) => cleanTag(t.tag)))] : []);
</script>

<svelte:window onkeydown={keydown} />

<div class="lightbox" role="dialog" aria-modal="true" aria-label={item?.original_filename ?? 'Media'}>
	<div class="top">
		<!-- Clicking the backdrop around the media closes, like Esc (the keyboard path). -->
		<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
		<div class="stage" onclick={(e) => e.target === e.currentTarget && onclose()}>
			{#if error}
				<p class="muted">{error}</p>
			{:else if item}
				{#if item.kind === 'video'}
					<!-- svelte-ignore a11y_media_has_caption -->
					<video bind:this={video} src={rawUrl(item.sha256)} controls autoplay loop></video>
				{:else}
					<img src={rawUrl(item.sha256)} alt={item.original_filename} />
				{/if}
			{/if}
			{#if onprev}<button class="nav prev" onclick={onprev} aria-label="Previous">‹</button>{/if}
			{#if onnext}<button class="nav next" onclick={onnext} aria-label="Next">›</button>{/if}
		</div>

		<aside class="info">
			<div class="head">
				<h2>{item?.original_filename ?? ''}</h2>
				<button class="close" onclick={onclose} aria-label="Close (Esc)">✕</button>
			</div>

			{#if item}
				<div class="actions">
					<a class="btn btn-primary" href={rawUrl(item.sha256)} download={item.original_filename}>Download</a>
					{#if confirming}
						<span class="confirm">
							Remove from your library?
							<button class="btn-quiet danger" onclick={remove}>Remove</button>
							<button class="btn-quiet" onclick={() => (confirming = false)}>Cancel</button>
						</span>
					{:else}
						<button class="btn-quiet" onclick={() => (confirming = true)}>Remove</button>
					{/if}
				</div>

				<dl>
					<dt>Type</dt>
					<dd>{item.kind} <span class="muted mono">{item.mime_type}</span></dd>
					<dt>Size</dt>
					<dd class="mono">{formatBytes(item.size_bytes)}</dd>
					{#if item.width}
						<dt>Dimensions</dt>
						<dd class="mono">{item.width} × {item.height}</dd>
					{/if}
					{#if item.duration_s}
						<dt>Duration</dt>
						<dd class="mono">{formatDuration(item.duration_s)}</dd>
					{/if}
					<dt>Uploaded</dt>
					<dd class="mono">{isoMinute(item.uploaded_at)}</dd>
					{#if item.source_device}
						<dt>Device</dt>
						<dd class="mono">{item.source_device}</dd>
					{/if}
					{#if item.status !== 'ready'}
						<dt>Status</dt>
						<dd>{item.status}{#if item.error}<span class="err">{item.error}</span>{/if}</dd>
					{/if}
					<dt>SHA-256</dt>
					<dd class="hash">
						<span class="mono" title={item.sha256}>{shortHash(item.sha256)}</span>
						<button class="btn-quiet" onclick={copyHash}>{copied ? 'Copied' : 'Copy'}</button>
					</dd>
				</dl>

				{#if tags.length}
					<section>
						<h3 class="label">Tags</h3>
						<div class="chips">
							{#each tags as t (t)}
								<a class="chip" href={`/?q=${encodeURIComponent(t)}`}>{t}</a>
							{/each}
						</div>
					</section>
				{/if}
				{#if ocr.length}
					<section>
						<h3 class="label">Text in image</h3>
						{#each ocr as line (line)}<p class="quote">{line}</p>{/each}
					</section>
				{/if}
				{#if item.transcript}
					<section>
						<h3 class="label">Transcript</h3>
						<p class="quote">{item.transcript}</p>
					</section>
				{/if}
				{#if item.kind === 'video' && item.frames.length > 1}
					<section>
						<h3 class="label">Keyframes</h3>
						<div class="chips">
							{#each item.frames as f (f.idx)}
								<button class="chip mono" onclick={() => video && ((video.currentTime = f.ts_s), video.play())}>
									{formatDuration(f.ts_s)}
								</button>
							{/each}
						</div>
					</section>
				{/if}
			{/if}
		</aside>
	</div>

	{#if item?.duplicates?.length}
		<section class="below">
			<h3 class="section-title">Near-duplicates <span class="muted mono">{item.duplicates.length}</span></h3>
			<JustifiedGrid items={item.duplicates} onopen={onnavigate} />
		</section>
	{/if}
	{#if similar.length}
		<section class="below">
			<h3 class="section-title">Similar</h3>
			<JustifiedGrid items={similar} onopen={onnavigate} />
		</section>
	{/if}
</div>

<style>
	.lightbox {
		position: fixed;
		inset: 0;
		z-index: 10;
		overflow-y: auto;
		background: var(--c-overlay);
		backdrop-filter: blur(6px);
	}
	.top {
		display: grid;
		grid-template-columns: minmax(0, 1fr) 360px;
		height: 100vh;
		height: 100dvh;
	}
	/* The stage height is set explicitly: max-height: 100% on the media does not
	   resolve against an auto-height grid row, so a tall image would render at
	   full size and run over the sections below. */
	.stage {
		--stage-h: 100dvh;
		--stage-pad: var(--sp-5);
		position: relative;
		display: grid;
		place-items: center;
		height: var(--stage-h);
		min-height: 0;
		padding: var(--stage-pad);
		overflow: hidden;
		cursor: zoom-out;
	}
	.stage > :global(*) {
		cursor: auto;
	}
	.stage img,
	.stage video {
		max-width: 100%;
		max-height: calc(var(--stage-h) - 2 * var(--stage-pad));
		object-fit: contain;
		border-radius: var(--r-sm);
		background: var(--c-surface);
	}
	.nav {
		position: absolute;
		top: 50%;
		translate: 0 -50%;
		width: 44px;
		height: 64px;
		border: 0;
		border-radius: var(--r-md);
		background: rgb(255 255 255 / 0.06);
		color: var(--c-text);
		font-size: 2rem;
		cursor: pointer;
	}
	.nav:hover {
		background: rgb(255 255 255 / 0.12);
	}
	.prev {
		left: var(--sp-3);
	}
	.next {
		right: var(--sp-3);
	}
	.info {
		overflow-y: auto;
		padding: var(--sp-4) var(--sp-5);
		background: var(--c-surface);
		border-left: 1px solid var(--c-border);
	}
	.head {
		display: flex;
		align-items: flex-start;
		gap: var(--sp-3);
	}
	h2 {
		flex: 1;
		font-family: var(--font-display);
		font-size: var(--text-lg);
		word-break: break-word;
	}
	.close {
		flex: none;
		width: 32px;
		height: 32px;
		border: 0;
		border-radius: var(--r-md);
		background: var(--c-surface-2);
		color: var(--c-muted);
		cursor: pointer;
	}
	.close:hover {
		color: var(--c-text);
	}
	.actions {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--sp-4);
		margin: var(--sp-4) 0;
	}
	.confirm {
		display: inline-flex;
		align-items: center;
		gap: var(--sp-3);
		color: var(--c-muted);
		font-size: var(--text-sm);
	}
	dl {
		display: grid;
		grid-template-columns: auto 1fr;
		gap: var(--sp-2) var(--sp-4);
		margin: 0 0 var(--sp-5);
		font-size: var(--text-sm);
	}
	dt {
		color: var(--c-subtle);
	}
	dd {
		margin: 0;
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.hash {
		display: flex;
		gap: var(--sp-3);
		align-items: baseline;
	}
	.err {
		display: block;
		color: var(--c-danger);
		font-size: var(--text-xs);
	}
	section {
		margin-bottom: var(--sp-5);
	}
	section .label {
		margin-bottom: var(--sp-2);
		font-weight: 500;
	}
	.chips {
		display: flex;
		flex-wrap: wrap;
		gap: var(--sp-1);
	}
	.chip {
		padding: 2px var(--sp-2);
		border: 1px solid var(--c-border);
		border-radius: var(--r-pill);
		background: none;
		color: var(--c-muted);
		font-size: var(--text-xs);
		cursor: pointer;
	}
	.chip:hover {
		border-color: var(--c-accent);
		color: var(--c-text);
	}
	.quote {
		margin: 0 0 var(--sp-2);
		padding: var(--sp-2) var(--sp-3);
		border-radius: var(--r-md);
		background: var(--c-surface-2);
		font-size: var(--text-sm);
		white-space: pre-wrap;
	}
	.below {
		padding: var(--sp-5) var(--sp-5) 0;
		background: var(--c-bg);
	}
	.below:last-child {
		padding-bottom: var(--sp-6);
	}
	@media (max-width: 860px) {
		.top {
			grid-template-columns: 1fr;
			height: auto;
		}
		.stage {
			--stage-h: 62dvh;
			--stage-pad: var(--sp-2);
		}
		.info {
			border-left: 0;
			padding: var(--sp-4) var(--gutter);
		}
		.below {
			padding-inline: var(--gutter);
		}
		.nav {
			width: 36px;
			height: 52px;
		}
	}
</style>
