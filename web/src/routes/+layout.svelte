<script lang="ts">
	import '../app.css';
	import { onMount } from 'svelte';
	import { api } from '#lib/api.ts';
	import { session, loadSession } from '#lib/session.svelte.ts';

	let { children } = $props();

	onMount(loadSession);

	async function logout() {
		await api.logout();
		session.user = null;
	}
</script>

<header>
	<a class="brand" href="/">🐸 Pepes Bodega</a>
	{#if session.user}
		<nav>
			<a href="/">Search</a>
			<a href="/upload">Upload</a>
			<a href="/settings">Settings</a>
			<span class="muted">{session.user.name || session.user.email}</span>
			<button onclick={logout}>Log out</button>
		</nav>
	{/if}
</header>

{#if !session.checked}
	<main><p class="muted">Opening the bodega…</p></main>
{:else if !session.user}
	<main class="login">
		<h1>Welcome to Pepes Bodega</h1>
		<p class="muted">Your meme & reaction archive. Ingen sangria utan inloggning.</p>
		<a href="/auth/login"><button class="primary">Log in</button></a>
	</main>
{:else}
	{@render children()}
{/if}

<style>
	header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 1rem;
		padding: 0.7rem 1rem;
		background: var(--panel);
		border-bottom: 2px solid var(--accent-2);
	}
	.brand {
		font-weight: 800;
		font-size: 1.2rem;
		color: var(--accent-2);
	}
	nav {
		display: flex;
		align-items: center;
		gap: 1rem;
	}
	.login {
		text-align: center;
		padding-top: 15vh;
	}
</style>
