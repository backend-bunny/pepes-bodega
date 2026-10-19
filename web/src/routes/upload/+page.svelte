<script lang="ts">
	import { upload } from '#lib/api.ts';

	interface Job {
		file: File;
		progress: number;
		state: 'queued' | 'uploading' | 'done' | 'error';
		message: string;
		mediaId?: number;
	}

	const ACCEPT = 'image/*,video/*,.gif,.webm,.mkv,.mov';
	const PARALLEL = 3;

	let jobs = $state<Job[]>([]);
	let dragging = $state(false);

	function add(files: FileList | null) {
		if (!files) return;
		for (const file of files) {
			if (file.type.startsWith('image/') || file.type.startsWith('video/')) {
				jobs.push({ file, progress: 0, state: 'queued', message: '' });
			}
		}
		pump();
	}

	function pump() {
		const active = jobs.filter((j) => j.state === 'uploading').length;
		for (const job of jobs.filter((j) => j.state === 'queued').slice(0, PARALLEL - active)) {
			job.state = 'uploading';
			upload(job.file, (p) => (job.progress = p))
				.then((r) => {
					job.state = 'done';
					job.mediaId = r.media_id;
					job.message = !r.created ? 'already in your library' : r.duplicate ? 'already in the archive' : 'queued for processing';
				})
				.catch((e) => {
					job.state = 'error';
					job.message = String(e.message ?? e);
				})
				.finally(pump);
		}
	}

	function drop(e: DragEvent) {
		e.preventDefault();
		dragging = false;
		add(e.dataTransfer?.files ?? null);
	}
</script>

<main>
	<h1>Upload</h1>
	<label
		class="drop"
		class:dragging
		ondragover={(e) => {
			e.preventDefault();
			dragging = true;
		}}
		ondragleave={() => (dragging = false)}
		ondrop={drop}
	>
		<input type="file" multiple accept={ACCEPT} onchange={(e) => add(e.currentTarget.files)} hidden />
		<strong>Drop memes here</strong> or click to choose files
		<span class="muted">For whole folders and other devices use the <code>bodega</code> CLI (see Settings).</span>
	</label>

	<ul>
		{#each jobs as job, i (i)}
			<li class={job.state}>
				<span class="name">{job.file.name}</span>
				{#if job.state === 'uploading'}
					<progress value={job.progress}></progress>
				{:else if job.state === 'done'}
					<a href={`/m/${job.mediaId}`}>✓ {job.message}</a>
				{:else if job.state === 'error'}
					<span class="err">✗ {job.message}</span>
				{:else}
					<span class="muted">queued</span>
				{/if}
			</li>
		{/each}
	</ul>
</main>

<style>
	.drop {
		display: flex;
		flex-direction: column;
		gap: 0.4rem;
		align-items: center;
		padding: 3rem;
		border: 2px dashed #4a3f37;
		border-radius: var(--radius);
		cursor: pointer;
	}
	.drop.dragging {
		border-color: var(--accent);
		background: var(--panel);
	}
	ul {
		list-style: none;
		padding: 0;
	}
	li {
		display: flex;
		justify-content: space-between;
		gap: 1rem;
		padding: 0.35rem 0;
		border-bottom: 1px solid var(--panel-2);
	}
	.name {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.err {
		color: var(--danger);
	}
</style>
