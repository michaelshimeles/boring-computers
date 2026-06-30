<script lang="ts">
	import { page } from '$app/state';

	const title = 'Boring Computers';
	const description = 'Computers that are refreshingly boring.';

	// Absolute URLs so social scrapers resolve the image on any deployed domain.
	const ogImage = $derived(`${page.url.origin}/og.png`);
	const ogUrl = $derived(page.url.href);
</script>

<svelte:head>
	<title>{title}</title>
	<meta name="description" content={description} />

	<!-- OpenGraph -->
	<meta property="og:type" content="website" />
	<meta property="og:site_name" content="Boring Computers" />
	<meta property="og:url" content={ogUrl} />
	<meta property="og:title" content={title} />
	<meta property="og:description" content={description} />
	<meta property="og:image" content={ogImage} />
	<meta property="og:image:width" content="1200" />
	<meta property="og:image:height" content="630" />
	<meta
		property="og:image:alt"
		content="boring computers — Computers that are refreshingly boring."
	/>

	<!-- Twitter -->
	<meta name="twitter:card" content="summary_large_image" />
	<meta name="twitter:title" content={title} />
	<meta name="twitter:description" content={description} />
	<meta name="twitter:image" content={ogImage} />
</svelte:head>

<div class="flex min-h-screen items-center justify-center bg-black px-5">
	<h1
		class="text-center text-[clamp(1rem,3vw,2rem)] font-semibold whitespace-nowrap tracking-[-0.03em] text-ink"
	>
		Computers that are
		<span class="text-ink-subtle">refreshingly boring.</span>
	</h1>
</div>
