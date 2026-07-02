<script lang="ts">
	import { onMount } from 'svelte';
	import { apiBase, wsUrl } from '$lib/boring';

	type Machine = { id: string; mode: string; boot_ms: number };
	type Phase = 'booting' | 'connecting' | 'live' | 'done' | 'error';

	let { onClose }: { onClose?: () => void } = $props();

	// A deliberately simple, reliable task for the minimal desktop (terminal + clock).
	const GOAL =
		"Click the terminal window to focus it, then run a command that prints today's date and a cheerful one-line hello from boring computers.";
	const TTL = 240;
	const MAX_ATTEMPTS = 10;

	let phase = $state<Phase>('booting');
	let machine = $state<Machine | null>(null);
	let error = $state('');
	let caption = $state('Booting a computer for the AI…');
	let log = $state<{ kind: string; text: string }[]>([]);

	let screen: HTMLDivElement;
	// eslint-disable-next-line @typescript-eslint/no-explicit-any
	let rfb: any = null;
	let ws: WebSocket | null = null;
	let attempts = 0;
	let disposed = false;
	let agentStarted = false;

	onMount(() => {
		void launch();
		return () => close();
	});

	async function launch() {
		try {
			const res = await fetch(`${apiBase}/v1/machines`, {
				method: 'POST',
				headers: { 'content-type': 'application/json' },
				body: JSON.stringify({ template: 'desktop', ttl_seconds: TTL })
			});
			if (!res.ok) throw new Error(`control plane returned ${res.status}`);
			machine = await res.json();
			phase = 'connecting';
			caption = 'Starting the display…';
			// Let X paint before noVNC's first full frame; the agent starts on connect.
			setTimeout(connectVNC, 4500);
		} catch (e) {
			error = e instanceof Error ? e.message : String(e);
			phase = 'error';
		}
	}

	function teardownRfb() {
		try {
			rfb?.disconnect();
		} catch {
			/* ignore */
		}
		rfb = null;
		// eslint-disable-next-line svelte/no-dom-manipulating
		if (screen) screen.innerHTML = '';
	}

	async function connectVNC() {
		if (disposed || !machine) return;
		attempts += 1;
		const { default: RFB } = await import('@novnc/novnc');
		if (disposed) return;
		teardownRfb();
		try {
			rfb = new RFB(screen, wsUrl(`/v1/machines/${machine.id}/vnc`), {});
			rfb.scaleViewport = true;
			rfb.resizeSession = false;
			rfb.background = '#000';
			rfb.viewOnly = true; // the AI drives; the human just watches
			rfb.addEventListener('connect', () => {
				// x11vnc accepts before the apps finish painting; give X a moment so
				// the agent's first screenshot shows the desktop, not a black frame.
				if (!disposed) setTimeout(startAgent, 2500);
			});
			rfb.addEventListener('disconnect', () => {
				if (disposed) return;
				if (!agentStarted && attempts < MAX_ATTEMPTS) setTimeout(connectVNC, 1500);
			});
		} catch {
			if (attempts < MAX_ATTEMPTS) setTimeout(connectVNC, 1500);
		}
	}

	function startAgent() {
		if (agentStarted || disposed || !machine) return;
		agentStarted = true;
		phase = 'live';
		caption = 'The AI is looking at the screen…';
		ws = new WebSocket(wsUrl(`/v1/machines/${machine.id}/agent?goal=${encodeURIComponent(GOAL)}`));
		ws.onmessage = (e) => {
			let m: { type: string; text?: string };
			try {
				m = JSON.parse(e.data);
			} catch {
				return;
			}
			if (m.type === 'say' && m.text) {
				caption = m.text;
				log = [...log, { kind: 'say', text: m.text }].slice(-6);
			} else if (m.type === 'action' && m.text) {
				log = [...log, { kind: 'action', text: m.text }].slice(-6);
			} else if (m.type === 'done') {
				phase = 'done';
				caption = m.text || 'The AI finished the task.';
			} else if (m.type === 'error') {
				phase = 'error';
				error = m.text || 'the agent stopped unexpectedly';
			}
		};
		ws.onclose = () => {
			if (phase === 'live') {
				phase = 'done';
				caption = 'The AI finished.';
			}
		};
	}

	export function close() {
		disposed = true;
		try {
			ws?.close();
		} catch {
			/* ignore */
		}
		ws = null;
		try {
			rfb?.disconnect();
		} catch {
			/* ignore */
		}
		rfb = null;
		if (machine) {
			void fetch(`${apiBase}/v1/machines/${machine.id}`, { method: 'DELETE' }).catch(() => {});
		}
		machine = null;
		onClose?.();
	}

	function onKey(e: KeyboardEvent) {
		if (e.key === 'Escape') close();
	}
</script>

<svelte:window onkeydown={onKey} />

<div class="w-full max-w-3xl">
	<div
		class="flex items-center justify-between rounded-t-geist-lg border border-line bg-surface px-4 py-2.5 font-mono text-[12px]"
	>
		<div class="flex items-center gap-2 text-ink-muted">
			{#if phase === 'booting' || phase === 'connecting'}
				<span class="size-1.5 animate-pulse rounded-full bg-ink-subtle"></span>preparing a computer…
			{:else if phase === 'live'}
				<span class="size-1.5 animate-pulse rounded-full bg-accent"></span>
				<span class="text-ink">an AI is using this computer</span>
			{:else if phase === 'done'}
				<span class="size-1.5 rounded-full bg-success"></span>finished
			{:else if phase === 'error'}
				<span class="size-1.5 rounded-full bg-danger"></span>
				<span class="text-danger">{error}</span>
			{/if}
		</div>
		<button class="text-ink-subtle transition-colors hover:text-ink" onclick={close}>esc ✕</button>
	</div>
	<div
		class="relative overflow-hidden border-x border-line bg-black"
		class:hidden={phase === 'error'}
	>
		<div bind:this={screen} class="aspect-[16/10] w-full"></div>
		{#if phase !== 'live' && phase !== 'done'}
			<div
				class="pointer-events-none absolute inset-0 flex items-center justify-center font-mono text-[12px] text-ink-subtle"
			>
				allocating a computer…
			</div>
		{/if}
	</div>
	<!-- caption strip: the AI narrates what it's doing -->
	<div
		class="flex items-start gap-2.5 rounded-b-geist-lg border border-t-0 border-line bg-surface px-4 py-3 font-mono text-[12px]"
		class:hidden={phase === 'error'}
	>
		<span class="mt-px shrink-0 text-accent">✦</span>
		<span class="leading-relaxed text-ink-muted">{caption}</span>
	</div>
</div>
