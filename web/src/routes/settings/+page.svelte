<script lang="ts">
	import { onMount } from 'svelte';
	import { api, type ApiToken } from '#lib/api.ts';

	let tokens = $state<ApiToken[]>([]);
	let name = $state('');
	let secret = $state('');
	let error = $state('');

	const load = () => api.tokens().then((r) => (tokens = r.items)).catch((e) => (error = String(e)));
	onMount(load);

	async function create(e: SubmitEvent) {
		e.preventDefault();
		error = '';
		try {
			const res = await api.createToken(name.trim());
			secret = res.secret;
			name = '';
			await load();
		} catch (err) {
			error = String(err);
		}
	}

	async function revoke(t: ApiToken) {
		if (!confirm(`Revoke token "${t.name}"? Devices using it will stop syncing.`)) return;
		await api.deleteToken(t.id);
		await load();
	}
</script>

<main>
	<h1>Settings</h1>
	<h2>Device tokens</h2>
	<p class="muted">Create one token per device that runs the <code>bodega</code> uploader.</p>

	<form onsubmit={create}>
		<input type="text" bind:value={name} placeholder="Token name, e.g. laptop" required />
		<button class="primary">Create token</button>
	</form>

	{#if secret}
		<div class="secret">
			<p>Copy this now, it won't be shown again:</p>
			<pre>{secret}</pre>
			<p>On the device:</p>
			<pre>bodega login --server {location.origin} --token {secret}
bodega watch ~/Pictures/memes</pre>
		</div>
	{/if}
	{#if error}<p class="err">{error}</p>{/if}

	<table>
		<thead><tr><th>Name</th><th>Created</th><th>Last used</th><th></th></tr></thead>
		<tbody>
			{#each tokens as t (t.id)}
				<tr>
					<td>{t.name}</td>
					<td>{new Date(t.created_at).toLocaleDateString()}</td>
					<td>{t.last_used_at ? new Date(t.last_used_at).toLocaleString() : 'never'}</td>
					<td><button class="danger" onclick={() => revoke(t)}>Revoke</button></td>
				</tr>
			{:else}
				<tr><td colspan="4" class="muted">No tokens yet.</td></tr>
			{/each}
		</tbody>
	</table>
</main>

<style>
	form {
		display: flex;
		gap: 0.5rem;
		margin-bottom: 1rem;
	}
	.secret {
		background: var(--panel);
		border: 1px solid var(--accent-2);
		border-radius: var(--radius);
		padding: 0.5rem 1rem;
		margin-bottom: 1rem;
	}
	pre {
		white-space: pre-wrap;
		word-break: break-all;
	}
	table {
		width: 100%;
		border-collapse: collapse;
	}
	th,
	td {
		text-align: left;
		padding: 0.4rem;
		border-bottom: 1px solid var(--panel-2);
	}
	.err {
		color: var(--danger);
	}
</style>
