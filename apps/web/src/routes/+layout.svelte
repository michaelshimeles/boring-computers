<script lang="ts">
	import './layout.css';
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import favicon from '$lib/assets/favicon.svg';

	let { children } = $props();

	const SITE = 'https://boringcomputers.com';
	const OG_DESC =
		'Instant Firecracker microVMs with a terminal, a real browser, coding agents preinstalled, and an AI that drives them.';
</script>

<svelte:head>
	<link rel="icon" href={favicon} />
	<meta property="og:site_name" content="Boring Computers" />
	<meta property="og:type" content="website" />
	<meta property="og:title" content="Computers that are refreshingly boring." />
	<meta property="og:description" content={OG_DESC} />
	<meta property="og:url" content="{SITE}{page.url.pathname}" />
	<meta property="og:image" content="{SITE}/og.png" />
	<meta property="og:image:width" content="1200" />
	<meta property="og:image:height" content="630" />
	<meta name="twitter:card" content="summary_large_image" />
	<meta name="twitter:title" content="Computers that are refreshingly boring." />
	<meta name="twitter:description" content={OG_DESC} />
	<meta name="twitter:image" content="{SITE}/og.png" />
</svelte:head>

<nav
	class="max-w-4xl mx-auto absolute inset-x-0 top-0 z-20 flex items-center justify-between px-5 py-4 font-mono text-[12px]"
>
	<a href={resolve('/')} class="font-semibold text-ink transition-opacity hover:opacity-80 p-2"
		>boring.</a
	>
	<div class="flex items-center gap-5">
		<a href={resolve('/inference')} class="text-ink-subtle transition-colors hover:text-ink"
			>Inference</a
		>
		<a href={resolve('/docs')} class="text-ink-subtle transition-colors hover:text-ink">Docs</a>
	</div>
</nav>

{@render children()}
