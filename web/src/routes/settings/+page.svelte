<script lang="ts">
	import { onMount } from 'svelte';
	import { api, type ApiToken } from '#lib/api.ts';
	import { copyText, isoMinute, relativeTime } from '#lib/format.ts';

	let tokens = $state<ApiToken[]>([]);
	let name = $state('');
	let command = $state(''); // shown once after creating a token
	let copied = $state(false);
	let confirming = $state<number | null>(null);
	let error = $state('');

	const load = () => api.tokens().then((r) => (tokens = r.items)).catch((e) => (error = String(e)));
	onMount(load);

	async function create(e: SubmitEvent) {
		e.preventDefault();
		error = '';
		try {
			const res = await api.createToken(name.trim());
			command = `bodega login --server ${location.origin} --token ${res.secret}`;
			copied = false;
			name = '';
			await load();
		} catch (err) {
			error = String(err);
		}
	}

	async function copy() {
		copied = await copyText(command);
	}

	async function revoke(t: ApiToken) {
		await api.deleteToken(t.id);
		confirming = null;
		await load();
	}
</script>

<main class="page page-narrow">
	<h1 class="section-title">Device tokens</h1>
	<p class="muted intro">
		One token per device that runs the <span class="mono">bodega</span> uploader. Tokens can upload and search
		your library; revoke them when a device is gone.
	</p>

	<form onsubmit={create} autocomplete="off">
		<!-- Not a credential: keep password managers from offering to fill or save it. -->
		<input
			type="text"
			name="bodega-device-label"
			bind:value={name}
			placeholder="Device name, e.g. laptop"
			aria-label="Device name"
			autocomplete="off"
			spellcheck="false"
			data-1p-ignore
			data-lpignore="true"
			data-bwignore
			data-form-type="other"
			required
		/>
		<button class="btn btn-primary">Create token</button>
	</form>

	{#if command}
		<div class="once">
			<p>Run this on the device. The token is shown only once.</p>
			<div class="cmd">
				<code class="mono">{command}</code>
				<button class="btn" onclick={copy}>{copied ? 'Copied' : 'Copy'}</button>
			</div>
			<p class="muted">Then keep a folder in sync: <span class="mono">bodega watch ~/Pictures/memes</span></p>
			<button class="btn-quiet" onclick={() => (command = '')}>Done</button>
		</div>
	{/if}
	{#if error}<p class="err">{error}</p>{/if}

	<ul class="tokens">
		{#each tokens as t (t.id)}
			<li>
				<div class="tname mono">{t.name}</div>
				<div class="meta muted">
					created <time datetime={t.created_at} title={isoMinute(t.created_at)}>{relativeTime(t.created_at)}</time>
					· {#if t.last_used_at}used
						<time datetime={t.last_used_at} title={isoMinute(t.last_used_at)}>{relativeTime(t.last_used_at)}</time
						>{:else}never used{/if}
				</div>
				<div class="act">
					{#if confirming === t.id}
						<span class="muted">Revoke “{t.name}”?</span>
						<button class="btn-quiet danger" onclick={() => revoke(t)}>Revoke</button>
						<button class="btn-quiet" onclick={() => (confirming = null)}>Cancel</button>
					{:else}
						<button class="btn-quiet" onclick={() => (confirming = t.id)}>Revoke</button>
					{/if}
				</div>
			</li>
		{:else}
			<li class="empty muted">No tokens yet.</li>
		{/each}
	</ul>
</main>

<style>
	.intro {
		margin: 0 0 var(--sp-4);
		font-size: var(--text-sm);
	}
	form {
		display: flex;
		gap: var(--sp-2);
		margin-bottom: var(--sp-4);
	}
	.once {
		margin-bottom: var(--sp-5);
		padding: var(--sp-3) var(--sp-4);
		border: 1px solid var(--c-accent);
		border-radius: var(--r-lg);
		background: var(--c-accent-soft);
		font-size: var(--text-sm);
	}
	.once p {
		margin: 0 0 var(--sp-2);
	}
	.cmd {
		display: flex;
		gap: var(--sp-2);
		align-items: flex-start;
		margin-bottom: var(--sp-2);
	}
	.cmd code {
		flex: 1;
		min-width: 0;
		padding: var(--sp-2) var(--sp-3);
		border-radius: var(--r-md);
		background: var(--c-bg);
		font-size: var(--text-xs);
		overflow-wrap: anywhere;
	}
	.tokens {
		margin: 0;
		padding: 0;
		list-style: none;
		border-top: 1px solid var(--c-border);
	}
	.tokens li {
		display: grid;
		grid-template-columns: minmax(0, 1fr) auto;
		gap: var(--sp-1) var(--sp-4);
		padding: var(--sp-3) 0;
		border-bottom: 1px solid var(--c-border);
	}
	.tname {
		font-size: var(--text-sm);
	}
	.meta {
		grid-column: 1;
		font-size: var(--text-xs);
	}
	.act {
		display: flex;
		gap: var(--sp-3);
		align-items: center;
		grid-row: 1 / span 2;
		grid-column: 2;
		font-size: var(--text-sm);
	}
	.tokens .empty {
		display: block;
		font-size: var(--text-sm);
	}
	.err {
		color: var(--c-danger);
	}
	@media (max-width: 600px) {
		.act .muted {
			display: none;
		}
	}
</style>
