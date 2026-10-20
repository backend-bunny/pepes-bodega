<script lang="ts">
	import { onMount } from 'svelte';
	import { api, subscribeProgress, thumbUrl, upload, type Status } from '#lib/api.ts';
	import { formatBytes } from '#lib/format.ts';

	type Stage = 'waiting' | 'uploading' | 'queued' | 'processing' | 'ready' | 'failed' | 'exists' | 'error';
	interface Job {
		key: number;
		file: File;
		stage: Stage;
		progress: number;
		message: string;
		sha256?: string;
		mediaId?: number;
	}

	const PARALLEL = 3;
	const STEPS: [Stage[], string][] = [
		[['uploading'], 'Upload'],
		[['queued'], 'Queue'],
		[['processing'], 'Analyse'],
		[['ready', 'exists'], 'Ready']
	];
	const ORDER: Stage[] = ['waiting', 'uploading', 'queued', 'processing', 'ready'];

	let jobs = $state<Job[]>([]);
	let dragging = $state(false);
	let picker: HTMLInputElement;
	let nextKey = 0;

	function isMedia(f: File) {
		return f.type.startsWith('image/') || f.type.startsWith('video/');
	}

	function add(files: Iterable<File>) {
		for (const f of files) {
			if (isMedia(f)) jobs.unshift({ key: nextKey++, file: f, stage: 'waiting', progress: 0, message: '' });
		}
		pump();
	}

	function stepState(job: Job, stages: Stage[]): 'done' | 'current' | 'todo' {
		if (stages.includes(job.stage)) return job.stage === 'ready' || job.stage === 'exists' ? 'done' : 'current';
		const at = ORDER.indexOf(job.stage === 'exists' ? 'ready' : job.stage);
		return at > ORDER.indexOf(stages[0]) ? 'done' : 'todo';
	}

	function fromStatus(s: Status): Stage {
		return s === 'pending' ? 'queued' : s;
	}

	function pump() {
		const active = jobs.filter((j) => j.stage === 'uploading').length;
		for (const job of jobs.filter((j) => j.stage === 'waiting').slice(0, PARALLEL - active)) {
			job.stage = 'uploading';
			upload(job.file, (p) => (job.progress = p))
				.then(async (r) => {
					job.sha256 = r.sha256;
					job.mediaId = r.media_id;
					if (!r.created) {
						job.stage = 'exists';
						job.message = 'already in your library';
						return;
					}
					// New to you, but the content may already be processed for someone else.
					job.stage = r.new_blob ? 'queued' : fromStatus((await api.get(r.media_id)).status);
				})
				.catch((e) => {
					job.stage = 'error';
					job.message = String(e.message ?? e);
				})
				.finally(pump);
		}
	}

	function paste(e: ClipboardEvent) {
		const files = [...(e.clipboardData?.files ?? [])];
		if (!files.length) return;
		e.preventDefault();
		const stamp = new Date().toISOString().slice(0, 19).replace(/[T:]/g, '-');
		add(
			files.map((f, i) =>
				// Screenshots paste as "image.png"; give them a findable name.
				f.name === 'image.png' || !f.name ? new File([f], `pasted-${stamp}-${i}.${f.type.split('/')[1]}`, { type: f.type }) : f
			)
		);
	}

	function drop(e: DragEvent) {
		e.preventDefault();
		dragging = false;
		add(e.dataTransfer?.files ?? []);
	}

	onMount(() =>
		subscribeProgress((ev) => {
			for (const job of jobs) {
				const s = job.sha256 && ev.blobs[job.sha256];
				if (s && job.stage !== 'exists') job.stage = fromStatus(s);
			}
		})
	);

	const counts = $derived({
		active: jobs.filter((j) => ['waiting', 'uploading', 'queued', 'processing'].includes(j.stage)).length,
		done: jobs.filter((j) => j.stage === 'ready' || j.stage === 'exists').length,
		failed: jobs.filter((j) => j.stage === 'failed' || j.stage === 'error').length
	});
</script>

<svelte:window
	onpaste={paste}
	ondragover={(e) => {
		e.preventDefault();
		dragging = true;
	}}
	ondragleave={(e) => e.relatedTarget === null && (dragging = false)}
	ondrop={drop}
/>

<main class="page page-narrow">
	<div class="drop" class:dragging>
		<div>
			<strong>Drop memes here</strong>
			<span class="muted">or paste with <kbd>Ctrl</kbd>+<kbd>V</kbd></span>
		</div>
		<button class="btn" onclick={() => picker.click()}>Choose files</button>
		<input
			bind:this={picker}
			type="file"
			multiple
			accept="image/*,video/*,.gif,.webm,.mkv,.mov"
			onchange={(e) => add(e.currentTarget.files ?? [])}
			hidden
		/>
	</div>
	<p class="hint muted">
		Whole folders and other devices: use the <span class="mono">bodega</span> CLI (see
		<a href="/settings">Settings</a>).
	</p>

	{#if jobs.length}
		<div class="summary muted">
			<span class="mono">{counts.active}</span> in progress · <span class="mono">{counts.done}</span> done{#if counts.failed}
				· <span class="mono failed">{counts.failed}</span> failed{/if}
		</div>
		<ul>
			{#each jobs as job (job.key)}
				<li>
					<div class="thumb">
						{#if (job.stage === 'ready' || job.stage === 'exists') && job.sha256}
							<img src={thumbUrl(job.sha256)} alt="" />
						{/if}
					</div>
					<div class="main">
						<div class="name">
							{#if job.mediaId && (job.stage === 'ready' || job.stage === 'exists')}
								<a href={`/m/${job.mediaId}`}>{job.file.name}</a>
							{:else}
								{job.file.name}
							{/if}
							<span class="mono muted size">{formatBytes(job.file.size)}</span>
						</div>
						{#if job.stage === 'error' || job.stage === 'failed'}
							<div class="msg failed">{job.stage === 'failed' ? 'processing failed' : job.message}</div>
						{:else}
							<ol class="steps">
								{#each STEPS as [stages, label] (label)}
									<li class={stepState(job, stages)}>
										{label}{#if stages[0] === 'uploading' && job.stage === 'uploading'}
											<span class="mono"> {Math.round(job.progress * 100)}%</span>{/if}
									</li>
								{/each}
							</ol>
							{#if job.message}<div class="msg muted">{job.message}</div>{/if}
						{/if}
					</div>
				</li>
			{/each}
		</ul>
	{/if}
</main>

<style>
	.drop {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--sp-4);
		padding: var(--sp-4) var(--sp-5);
		border: 1px dashed var(--c-border);
		border-radius: var(--r-lg);
		background: var(--c-surface);
	}
	.drop.dragging {
		border-color: var(--c-accent);
		background: var(--c-accent-soft);
	}
	.drop strong {
		display: block;
		font-family: var(--font-display);
		font-size: var(--text-lg);
	}
	kbd {
		padding: 0 var(--sp-1);
		border: 1px solid var(--c-border);
		border-radius: var(--r-sm);
		font-family: var(--font-mono);
		font-size: var(--text-xs);
	}
	.hint {
		margin: var(--sp-2) 0 var(--sp-5);
		font-size: var(--text-sm);
	}
	.hint a {
		color: var(--c-text);
		text-decoration: underline;
		text-decoration-color: var(--c-border);
	}
	.summary {
		margin-bottom: var(--sp-2);
		font-size: var(--text-sm);
	}
	ul {
		margin: 0;
		padding: 0;
		list-style: none;
	}
	ul > li {
		display: flex;
		gap: var(--sp-3);
		align-items: center;
		padding: var(--sp-2) 0;
		border-bottom: 1px solid var(--c-border);
	}
	.thumb {
		flex: none;
		width: 44px;
		height: 44px;
		overflow: hidden;
		border-radius: var(--r-sm);
		background: var(--c-surface-2);
	}
	.thumb img {
		width: 100%;
		height: 100%;
		object-fit: cover;
	}
	.main {
		flex: 1;
		min-width: 0;
	}
	.name {
		display: flex;
		gap: var(--sp-2);
		align-items: baseline;
		overflow: hidden;
		font-size: var(--text-sm);
		white-space: nowrap;
		text-overflow: ellipsis;
	}
	.name a:hover {
		color: var(--c-accent);
	}
	.size {
		font-size: var(--text-xs);
	}
	.steps {
		display: flex;
		gap: var(--sp-1);
		margin: var(--sp-1) 0 0;
		padding: 0;
		list-style: none;
		font-size: var(--text-xs);
	}
	.steps li {
		padding: 0 var(--sp-2);
		border: 1px dashed var(--c-border);
		border-radius: var(--r-pill);
		color: var(--c-subtle);
	}
	.steps li.current {
		border: 1px solid var(--c-accent);
		color: var(--c-accent-ink);
		background: var(--c-accent);
	}
	.steps li.done {
		border: 1px solid var(--c-surface-3);
		color: var(--c-text);
		background: var(--c-surface-3);
	}
	.steps li.done::before {
		content: '✓ ';
		color: var(--c-accent);
	}
	.msg {
		margin-top: var(--sp-1);
		font-size: var(--text-xs);
	}
	.failed {
		color: var(--c-danger);
	}
	@media (max-width: 600px) {
		.drop {
			padding: var(--sp-3) var(--sp-4);
		}
		.drop .muted {
			display: none;
		}
	}
</style>
