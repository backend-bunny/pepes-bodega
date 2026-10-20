<script lang="ts">
	import '../app.css';
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { api } from '#lib/api.ts';
	import { session, loadSession } from '#lib/session.svelte.ts';

	let { children } = $props();

	onMount(loadSession);

	const links = [
		{ href: '/', label: 'Search', match: (p: string) => p === '/' || p.startsWith('/m/') },
		{ href: '/upload', label: 'Upload', match: (p: string) => p.startsWith('/upload') },
		{ href: '/settings', label: 'Settings', match: (p: string) => p.startsWith('/settings') }
	];

	async function logout() {
		await api.logout();
		session.user = null;
	}
</script>

<header>
	<a class="brand" href="/">Pepes <span>Bodega</span></a>
	{#if session.user}
		<nav aria-label="Main">
			{#each links as l (l.href)}
				<a href={l.href} aria-current={l.match(page.url.pathname) ? 'page' : undefined}>{l.label}</a>
			{/each}
		</nav>
		<div class="who">
			<span class="muted">{session.user.name || session.user.email}</span>
			<button class="btn-quiet" onclick={logout}>Log out</button>
		</div>
	{/if}
</header>

{#if !session.checked}
	<main class="page"><p class="muted">Opening the bodega…</p></main>
{:else if !session.user}
	<main class="page login">
		<h1>Pepes <span>Bodega</span></h1>
		<p class="muted">Your meme and reaction archive. Ingen sangria utan inloggning.</p>
		<a class="btn btn-primary" href="/auth/login">Log in</a>
	</main>
{:else}
	{@render children()}
{/if}

<style>
	header {
		display: flex;
		align-items: center;
		gap: var(--sp-5);
		height: var(--header-h);
		max-width: var(--page-max);
		margin: 0 auto;
		padding: 0 var(--gutter);
	}
	.brand,
	.login h1 {
		font-family: var(--font-display);
		font-weight: 700;
		font-size: var(--text-lg);
		letter-spacing: -0.01em;
	}
	.brand span,
	.login h1 span {
		color: var(--c-accent);
	}
	nav {
		display: flex;
		gap: var(--sp-4);
	}
	.brand,
	nav a,
	.who {
		white-space: nowrap;
	}
	nav a {
		position: relative;
		padding: var(--sp-1) 0;
		color: var(--c-muted);
		font-size: var(--text-sm);
		font-weight: 500;
	}
	nav a:hover {
		color: var(--c-text);
	}
	nav a[aria-current='page'] {
		color: var(--c-text);
	}
	nav a[aria-current='page']::after {
		content: '';
		position: absolute;
		left: 0;
		right: 0;
		bottom: -2px;
		height: 2px;
		border-radius: var(--r-pill);
		background: var(--c-accent);
	}
	.who {
		display: flex;
		align-items: center;
		gap: var(--sp-3);
		margin-left: auto;
		font-size: var(--text-sm);
	}
	.login {
		display: grid;
		justify-items: center;
		gap: var(--sp-3);
		padding-top: 18vh;
		text-align: center;
	}
	.login h1 {
		font-size: 2.5rem;
	}
	@media (max-width: 600px) {
		header {
			gap: var(--sp-3);
		}
		nav {
			gap: var(--sp-3);
		}
		.brand {
			font-size: var(--text-base);
		}
		.who .muted {
			display: none;
		}
	}
</style>
