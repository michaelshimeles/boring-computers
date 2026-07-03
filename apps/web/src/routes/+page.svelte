<script lang="ts">
	import { onMount } from 'svelte';
	import { resolve } from '$app/paths';
	import Computer from '$lib/Computer.svelte';
	import Desktop from '$lib/Desktop.svelte';
	import Agent from '$lib/Agent.svelte';
	import { fleetCount } from '$lib/boring';

	type Mode = null | 'shell' | 'desktop' | 'agent';
	let mode = $state<Mode>(null);

	let fleet = $state(0);
	onMount(() => {
		const tick = async () => (fleet = await fleetCount());
		void tick();
		const t = setInterval(tick, 4000);
		return () => clearInterval(t);
	});

	// Session length for the shell + desktop (clamped server-side to 15–900s).
	const LENGTHS = [
		{ s: 60, l: '1 min' },
		{ s: 300, l: '5 min' },
		{ s: 900, l: '15 min' }
	];
	let ttl = $state(60);
	let net = $state(false); // internet on the shell (cold-boots; pip/npm/web work)

	const PRODUCTS = [
		{
			name: 'Sandboxes',
			desc: 'Headless microVMs with a serial console. python3 + node, ready in milliseconds.',
			live: true
		},
		{
			name: 'Computers',
			desc: 'Full Linux desktops streamed to your browser over VNC.',
			live: true
		},
		{
			name: 'Agents',
			desc: 'An AI that sees the screen and drives the mouse & keyboard.',
			live: true
		},
		{ name: 'Storage', desc: 'Persistent volumes that outlive the machine.', live: false },
		{ name: 'Inference', desc: 'One endpoint for every model.', live: false }
	];

	const HOW = [
		[
			'Firecracker microVMs',
			'Real hardware-virtualized isolation — a kernel per machine, not a shared container.'
		],
		[
			'Jailed & capped',
			'Each VM runs unprivileged and chrooted, with per-machine CPU, memory and PID limits.'
		],
		[
			'Snapshot-restore',
			'A shell resumes from a memory snapshot in ~3 ms. No cold boot, no waiting.'
		],
		[
			'Self-destruct',
			'Every machine has a TTL and cleans itself up. Nothing lingers, nothing to manage.'
		]
	];

	const STATS = [
		['~3 ms', 'snapshot boot'],
		['VM-grade', 'isolation, per machine'],
		['0', 'network egress for guests']
	];

	function onKeydown(e: KeyboardEvent) {
		if (e.key === 'Enter' && mode === null) {
			const el = document.activeElement;
			if (el && ['INPUT', 'TEXTAREA'].includes(el.tagName)) return;
			if (el?.closest('.xterm')) return;
			mode = 'shell';
		}
	}
</script>

<svelte:head>
	<title>Boring Computers</title>
	<meta name="description" content="Computers that are refreshingly boring." />
</svelte:head>

<svelte:window onkeydown={onKeydown} />

<div class="bg-black">
	<section class="flex min-h-screen flex-col items-center justify-center gap-8 px-5 py-16">
		<h1
			class="text-center text-[clamp(1rem,3vw,2rem)] font-semibold whitespace-nowrap tracking-[-0.03em] text-ink"
		>
			Computers that are
			<span class="text-ink-subtle">refreshingly boring.</span>
		</h1>

		{#if mode === 'shell'}
			<Computer {ttl} {net} onClose={() => (mode = null)} />
		{:else if mode === 'desktop'}
			<Desktop {ttl} onClose={() => (mode = null)} />
		{:else if mode === 'agent'}
			<Agent onClose={() => (mode = null)} />
		{:else}
			<div class="flex flex-col items-center gap-4">
				<div class="flex flex-col items-center gap-1.5">
					<button
						onclick={() => (mode = 'shell')}
						class="group inline-flex items-center gap-2 font-mono text-[13px] text-ink-subtle transition-colors hover:text-ink focus-visible:outline-none"
					>
						<kbd
							class="rounded-[5px] border border-line bg-surface px-1.5 py-0.5 text-ink-muted transition-colors group-hover:border-white/25"
							>⏎</kbd
						>
						<span
							>Press <span class="text-ink-muted group-hover:text-ink">enter</span> to get a computer</span
						>
						<span class="ml-0.5 inline-block h-3.5 w-1.5 animate-pulse bg-ink-subtle align-middle"
						></span>
					</button>
					<span class="font-mono text-[11px] text-ink-faint">python3 · node · full Linux</span>
				</div>

				<!-- session length -->
				<div class="flex items-center gap-1 font-mono text-[11px]">
					<span class="mr-1 text-ink-faint">session</span>
					{#each LENGTHS as opt (opt.s)}
						<button
							onclick={() => (ttl = opt.s)}
							class="rounded-full border px-2 py-0.5 transition-colors {ttl === opt.s
								? 'border-white/30 text-ink'
								: 'border-line text-ink-faint hover:text-ink-muted'}"
						>
							{opt.l}
						</button>
					{/each}
				</div>

				<!-- internet -->
				<div class="flex items-center gap-1 font-mono text-[11px]">
					<span class="mr-1 text-ink-faint">internet</span>
					<button
						onclick={() => (net = false)}
						class="rounded-full border px-2 py-0.5 transition-colors {!net
							? 'border-white/30 text-ink'
							: 'border-line text-ink-faint hover:text-ink-muted'}">off · instant</button
					>
					<button
						onclick={() => (net = true)}
						class="rounded-full border px-2 py-0.5 transition-colors {net
							? 'border-white/30 text-ink'
							: 'border-line text-ink-faint hover:text-ink-muted'}">on · pip · npm · web</button
					>
				</div>

				<div class="flex items-center gap-4">
					<button
						onclick={() => (mode = 'desktop')}
						class="font-mono text-[12px] text-ink-faint transition-colors hover:text-ink-muted focus-visible:outline-none"
					>
						or spin up a full desktop →
					</button>
					<button
						onclick={() => (mode = 'agent')}
						class="font-mono text-[12px] text-ink-faint transition-colors hover:text-ink-muted focus-visible:outline-none"
					>
						or watch an AI use one →
					</button>
				</div>

				{#if fleet > 0}
					<p class="font-mono text-[11px] text-ink-faint">
						<span class="text-success">●</span>
						{fleet}
						{fleet === 1 ? 'computer' : 'computers'} running right now
					</p>
				{/if}
			</div>
		{/if}
	</section>

	<!-- The lineup -->
	<section class="mx-auto max-w-4xl px-5 py-24">
		<h2 class="text-[13px] font-semibold tracking-wide text-ink-faint uppercase">The lineup</h2>
		<div
			class="mt-6 grid gap-px overflow-hidden rounded-geist-lg border border-line bg-line sm:grid-cols-2 lg:grid-cols-3"
		>
			{#each PRODUCTS as p (p.name)}
				<div class="flex flex-col bg-black p-6">
					<div class="flex items-center gap-2">
						<h3 class="text-[15px] font-semibold text-ink">{p.name}</h3>
						{#if p.live}
							<span class="font-mono text-[10px] text-success">● live</span>
						{:else}
							<span class="font-mono text-[10px] text-ink-faint">soon</span>
						{/if}
					</div>
					<p class="mt-2 text-[13px] leading-relaxed text-ink-muted">{p.desc}</p>
				</div>
			{/each}
		</div>
	</section>

	<!-- How it works -->
	<section class="mx-auto max-w-4xl px-5 py-24">
		<h2 class="text-[13px] font-semibold tracking-wide text-ink-faint uppercase">How it works</h2>
		<div class="mt-6 grid gap-8 sm:grid-cols-2">
			{#each HOW as [title, body] (title)}
				<div>
					<h3 class="text-[15px] font-semibold text-ink">{title}</h3>
					<p class="mt-1.5 text-[13px] leading-relaxed text-ink-muted">{body}</p>
				</div>
			{/each}
		</div>
	</section>

	<!-- Numbers -->
	<section class="mx-auto max-w-4xl px-5 py-24">
		<div class="grid gap-8 sm:grid-cols-3">
			{#each STATS as [n, label] (label)}
				<div>
					<div class="text-[32px] font-semibold tracking-[-0.03em] text-ink">{n}</div>
					<div class="mt-1 font-mono text-[12px] text-ink-faint">{label}</div>
				</div>
			{/each}
		</div>
	</section>

	<!-- Footer -->
	<footer class="mx-auto max-w-4xl px-5 py-16">
		<div
			class="flex flex-col gap-6 border-t border-line pt-10 sm:flex-row sm:items-center sm:justify-between"
		>
			<div>
				<div class="font-semibold text-ink">boring computers</div>
				<div class="mt-1 font-mono text-[12px] text-ink-faint">
					Computers that are refreshingly boring.
				</div>
			</div>
			<div class="flex items-center gap-5 font-mono text-[12px] text-ink-subtle">
				<a href={resolve('/docs')} class="transition-colors hover:text-ink">Docs</a>
				<span class="text-ink-faint">Pluto · Fabrika · Goshen Labs</span>
			</div>
		</div>
	</footer>
</div>
