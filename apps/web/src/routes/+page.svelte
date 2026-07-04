<script lang="ts">
	import { onMount } from 'svelte';
	import { resolve } from '$app/paths';
	import Workstation from '$lib/Workstation.svelte';
	import Chassis from '$lib/Chassis.svelte';
	import { fleetCount } from '$lib/boring';

	// One computer with everything on it: a live desktop (browser + GUI), a real
	// terminal (its serial shell, coding agents preinstalled), and an AI prompt
	// box that drives it. `launched` flips the whole thing on.
	let launched = $state(false);
	// ?restore=vol-… launches straight into a computer with that volume attached.
	let restore = $state<string | undefined>(undefined);

	let fleet = $state(0);
	onMount(() => {
		const vol = new URLSearchParams(location.search).get('restore');
		if (vol) {
			restore = vol;
			launched = true;
		}
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
	let ttl = $state(300);

	const PRODUCTS = [
		{
			name: 'Sandboxes',
			desc: 'Headless microVMs with a serial console + an AI that drives them. python3 · node · claude, milliseconds to boot, opt-in internet.',
			live: true
		},
		{
			name: 'Computers',
			desc: 'Full Linux desktops over VNC — a real browser plus claude, codex, cursor & pi preinstalled.',
			live: true
		},
		{
			name: 'Agents',
			desc: 'An AI that sees the screen and drives the mouse & keyboard.',
			live: true
		},
		{
			name: 'Inference',
			desc: 'One OpenAI-compatible endpoint for every model — Claude on Anthropic, the rest via OpenRouter.',
			live: true
		},
		{
			name: 'Storage',
			desc: 'Persistent volumes that outlive the machine — save a computer, restore it into a fresh one.',
			live: true
		}
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
		const el = document.activeElement;
		if (e.key === 'Enter' && !launched) {
			if (el && ['INPUT', 'TEXTAREA'].includes(el.tagName)) return;
			if (el?.closest('.xterm')) return;
			launched = true;
		} else if (e.key === 'Escape' && launched) {
			if (el?.closest('.xterm')) return; // esc belongs to the terminal (vim etc.)
			launched = false;
		}
	}
</script>

<svelte:head>
	<title>Boring Computers</title>
	<meta name="description" content="Computers that are refreshingly boring." />
</svelte:head>

<svelte:window onkeydown={onKeydown} />

<div class="bg-black p-2 max-w-4xl mx-auto">
	<section class="flex flex-col px-5 pt-22 pb-12">
		<div class="flex w-full flex-col gap-10">
			<div class="flex shrink-0 flex-col gap-2">
				<h1
					class="text-[clamp(1rem,3vw,2rem)] font-semibold whitespace-nowrap tracking-[-0.03em] text-ink"
				>
					Computers that are
					<span class="text-ink-subtle">refreshingly boring.</span>
				</h1>

				<!-- launcher -->
				<div class="flex w-full flex-col items-start gap-4">
					<p class="max-w-md font-mono text-[11px] leading-relaxed text-ink-faint">
						One machine, everything on it — a live desktop with a real browser, a terminal with
						claude · codex · cursor · pi preinstalled, and an AI you can hand the whole thing to.
					</p>

					<!-- config -->
					<div class="grid grid-cols-[72px_1fr] items-center gap-x-3 gap-y-3 font-mono text-[11px]">
						<span class="text-ink-faint">session</span>
						<div class="flex flex-wrap gap-1">
							{#each LENGTHS as opt (opt.s)}
								<button
									onclick={() => (ttl = opt.s)}
									class="rounded-full border px-2.5 py-0.5 transition-colors {ttl === opt.s
										? 'border-white/25 bg-white/10 text-ink'
										: 'border-line text-ink-faint hover:text-ink-muted'}"
								>
									{opt.l}
								</button>
							{/each}
						</div>
					</div>

					<!-- launch -->
					<div class="flex items-center gap-3 font-mono text-[11px]">
						{#if !launched}
							<button
								onclick={() => (launched = true)}
								class="rounded-geist bg-ink px-3 py-1.5 text-[12px] font-semibold text-black transition-opacity hover:opacity-90"
							>
								Launch computer
							</button>
							<span class="text-ink-faint"
								>or press <kbd
									class="rounded-[5px] border border-line bg-surface px-1.5 py-0.5 text-ink-muted"
									>⏎</kbd
								></span
							>
						{:else}
							<button
								onclick={() => (launched = false)}
								class="rounded-geist border border-line px-3 py-1.5 text-[12px] text-ink-subtle transition-colors hover:text-ink"
							>
								Shut down
							</button>
							<span class="text-ink-faint">esc closes it too</span>
						{/if}
					</div>

					{#if fleet > 0}
						<p class="font-mono text-[11px] text-ink-faint tabular-nums">
							<span class="text-success">●</span>
							{fleet}
							{fleet === 1 ? 'computer' : 'computers'} running right now
						</p>
					{/if}
				</div>
			</div>

			<div class="w-full">
				<Chassis on={launched}>
					{#if launched}
						<Workstation {ttl} volume={restore} onClose={() => (launched = false)} />
					{:else}
						<!-- powered-down screen: click it (or press enter) to boot -->
						<button
							onclick={() => (launched = true)}
							aria-label="Launch computer"
							class="flex h-full w-full cursor-pointer items-start p-4"
						>
							<span class="h-4 w-2 animate-pulse bg-ink-subtle"></span>
						</button>
					{/if}
				</Chassis>
			</div>
		</div>
	</section>

	<!-- The lineup -->
	<section class="px-5 py-12">
		<h2 class="text-[13px] font-semibold tracking-wide text-ink-faint uppercase">The lineup</h2>
		<div
			class="mt-6 grid gap-px overflow-hidden rounded-geist-lg border border-line bg-line sm:grid-cols-2 lg:grid-cols-3"
		>
			{#snippet card(p: { name: string; desc: string; live: boolean })}
				<div class="flex items-center gap-2">
					<h3 class="text-[15px] font-semibold text-ink">{p.name}</h3>
					{#if p.live}
						<span class="font-mono text-[10px] text-success">● live</span>
					{:else}
						<span class="font-mono text-[10px] text-ink-faint">soon</span>
					{/if}
				</div>
				<p class="mt-2 text-[13px] leading-relaxed text-ink-muted">{p.desc}</p>
			{/snippet}
			{#each PRODUCTS as p (p.name)}
				{#if p.name === 'Inference'}
					<a
						href={resolve('/inference')}
						class="flex flex-col bg-black p-6 transition-colors hover:bg-surface"
						>{@render card(p)}<span class="mt-2 font-mono text-[11px] text-accent"
							>open the playground →</span
						></a
					>
				{:else}
					<div class="flex flex-col bg-black p-6">{@render card(p)}</div>
				{/if}
			{/each}
			<!-- filler so the empty grid cell stays black instead of showing the bg-line backdrop -->
			<div class="hidden bg-black sm:block"></div>
		</div>
	</section>

	<!-- How it works -->
	<section class="px-5 py-12">
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
	<section class="px-5 py-12">
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
	<footer class="px-5 py-12">
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
