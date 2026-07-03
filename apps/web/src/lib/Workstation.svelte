<script lang="ts">
	import '@xterm/xterm/css/xterm.css';
	import { onMount } from 'svelte';
	import { apiBase, wsUrl, createMachine, getMachine, previewUrl, type Machine } from '$lib/boring';

	// One computer, everything on it: a live desktop (browser + GUI over VNC), its
	// serial shell as a real terminal (coding agents preinstalled), and a prompt
	// box that turns the whole thing over to an AI. All one desktop microVM.

	type Phase = 'booting' | 'connecting' | 'live' | 'error' | 'closed';
	let {
		onClose,
		ttl = 300,
		machineId
	}: { onClose?: () => void; ttl?: number; machineId?: string } = $props();

	let phase = $state<Phase>('booting');
	let machine = $state<Machine | null>(null);
	let error = $state('');
	let remaining = $state(0);
	let shared = $state(false);
	let copied = $state(false);
	let tab = $state<'desktop' | 'terminal'>('desktop');

	// VNC desktop
	let screen = $state<HTMLDivElement>();
	// eslint-disable-next-line @typescript-eslint/no-explicit-any
	let rfb: any = null;
	let attempts = 0;

	// Serial terminal
	let termHost = $state<HTMLDivElement>();
	// eslint-disable-next-line @typescript-eslint/no-explicit-any
	let term: any = null;
	// eslint-disable-next-line @typescript-eslint/no-explicit-any
	let fit: any = null;
	let tty: WebSocket | null = null;

	// AI prompt box (drives the computer-use agent on this machine)
	let goal = $state('');
	let agentRunning = $state(false);
	let agentLine = $state('');
	let agentWs: WebSocket | null = null;

	// Preview: open a port running inside this computer at a public URL.
	let previewPort = $state('');
	function openPreview() {
		const p = parseInt(previewPort, 10);
		if (!machine || !Number.isInteger(p) || p < 1 || p > 65535) return;
		window.open(previewUrl(machine.id, p), '_blank', 'noopener');
	}
	function previewKey(e: KeyboardEvent) {
		if (e.key === 'Enter') openPreview();
	}

	// File transfer: drag-drop to upload into /root, a path box to download.
	let dragOver = $state(false);
	let fileMsg = $state('');
	let dlPath = $state('');

	async function uploadFiles(files: FileList | File[]) {
		if (!machine) return;
		for (const f of Array.from(files)) {
			fileMsg = `⤒ uploading ${f.name}…`;
			try {
				const res = await fetch(`${apiBase}/v1/machines/${machine.id}/upload`, {
					method: 'POST',
					headers: { 'X-Filename': f.name },
					body: f
				});
				const j = await res.json().catch(() => ({}));
				fileMsg = res.ok ? `⤒ ${f.name} → ${j.path}` : `⚠ ${j.error ?? 'upload failed'}`;
			} catch {
				fileMsg = '⚠ upload failed';
			}
		}
		setTimeout(() => (fileMsg = ''), 5000);
	}

	async function downloadFile() {
		const p = dlPath.trim();
		if (!p || !machine) return;
		fileMsg = `⤓ fetching ${p}…`;
		try {
			const res = await fetch(
				`${apiBase}/v1/machines/${machine.id}/download?path=${encodeURIComponent(p)}`
			);
			if (!res.ok) {
				const j = await res.json().catch(() => ({}));
				fileMsg = `⚠ ${j.error ?? 'not found'}`;
			} else {
				const blob = await res.blob();
				const url = URL.createObjectURL(blob);
				const a = document.createElement('a');
				a.href = url;
				a.download = p.split('/').pop() || 'file';
				a.click();
				URL.revokeObjectURL(url);
				fileMsg = `⤓ downloaded ${a.download}`;
			}
		} catch {
			fileMsg = '⚠ download failed';
		}
		setTimeout(() => (fileMsg = ''), 5000);
	}

	function onDrop(e: DragEvent) {
		e.preventDefault();
		dragOver = false;
		if (e.dataTransfer?.files?.length) void uploadFiles(e.dataTransfer.files);
	}

	let countdown: ReturnType<typeof setInterval> | null = null;
	let onResize: (() => void) | null = null;
	let disposed = false;
	const MAX_ATTEMPTS = 10;

	onMount(() => {
		void launch();
		return () => close();
	});

	async function launch() {
		phase = 'booting';
		error = '';
		try {
			machine = machineId ? await getMachine(machineId) : await createMachine('desktop', ttl);
			phase = 'connecting';
			startCountdown();
			void openTerminal(machine.id); // the serial shell is up early
			// The desktop cold-boots X + chromium over a few seconds; a reconnect is
			// already painted.
			setTimeout(connectVNC, machineId ? 400 : 4500);
		} catch (e) {
			error = e instanceof Error ? e.message : String(e);
			phase = 'error';
		}
	}

	// --- desktop (noVNC) ---
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
		if (disposed || !machine || !screen) return;
		attempts += 1;
		const { default: RFB } = await import('@novnc/novnc');
		if (disposed) return;
		teardownRfb();
		try {
			rfb = new RFB(screen, wsUrl(`/v1/machines/${machine.id}/vnc`), {});
			rfb.scaleViewport = true;
			rfb.resizeSession = false;
			rfb.background = '#000';
			// The guest CPU is the bottleneck, not bandwidth: low zlib compression
			// means fast per-frame encoding (snappier), decent JPEG quality keeps
			// text readable. Tight encoding is negotiated with x11vnc automatically.
			rfb.qualityLevel = 7;
			rfb.compressionLevel = 1;
			rfb.addEventListener('connect', () => {
				if (!disposed) phase = 'live';
			});
			rfb.addEventListener('disconnect', () => {
				if (disposed) return;
				if (phase !== 'live' && attempts < MAX_ATTEMPTS) setTimeout(connectVNC, 1500);
				else if (phase === 'live') {
					phase = 'closed';
					stopCountdown();
				}
			});
		} catch {
			if (attempts < MAX_ATTEMPTS) setTimeout(connectVNC, 1500);
		}
	}

	// --- terminal (xterm over the guest serial) ---
	async function openTerminal(id: string) {
		const { Terminal } = await import('@xterm/xterm');
		const { FitAddon } = await import('@xterm/addon-fit');
		term = new Terminal({
			fontFamily: "'Geist Mono', ui-monospace, monospace",
			fontSize: 12,
			cursorBlink: true,
			theme: { background: '#0a0a0a', foreground: '#ededed', cursor: '#ededed', green: '#00ca50' }
		});
		fit = new FitAddon();
		term.loadAddon(fit);
		term.open(termHost!);
		fit.fit();
		onResize = () => fit?.fit();
		window.addEventListener('resize', onResize);

		tty = new WebSocket(wsUrl(`/v1/machines/${id}/tty`));
		tty.binaryType = 'arraybuffer';
		const enc = new TextEncoder();
		tty.onmessage = (e) => {
			if (e.data instanceof ArrayBuffer) term.write(new Uint8Array(e.data));
			else term.write(e.data);
		};
		term.onData((d: string) => tty?.readyState === WebSocket.OPEN && tty.send(enc.encode(d)));
		// Copy the selection with Cmd+C / Ctrl+Shift+C; bare Ctrl+C stays SIGINT.
		term.attachCustomKeyEventHandler((e: KeyboardEvent) => {
			if (
				e.type === 'keydown' &&
				e.key.toLowerCase() === 'c' &&
				(e.metaKey || (e.ctrlKey && e.shiftKey))
			) {
				const sel = term.getSelection();
				if (sel) {
					void navigator.clipboard?.writeText(sel).catch(() => {});
					return false;
				}
			}
			return true;
		});
		setTimeout(() => {
			if (!term) return;
			term.reset();
			term.write(
				'\x1b[38;5;244mboring computers · desktop shell · node · claude · codex · cursor · pi\r\n' +
					'run a server (e.g. python3 -m http.server 8000) then use "preview ↗" up top to open it\x1b[0m\r\n'
			);
			if (tty?.readyState === WebSocket.OPEN) tty.send(enc.encode('\n'));
		}, 500);
	}

	function showTerminal() {
		tab = 'terminal';
		setTimeout(() => {
			fit?.fit();
			term?.focus();
		}, 40);
	}

	// --- AI prompt box (computer-use agent on this desktop) ---
	function runAgent() {
		const g = goal.trim();
		if (!g || agentRunning || !machine || phase !== 'live') return;
		agentRunning = true;
		agentLine = 'looking at the screen…';
		tab = 'desktop';
		if (rfb) rfb.viewOnly = true; // the AI drives; you watch
		const w = new WebSocket(
			wsUrl(`/v1/machines/${machine.id}/agent?goal=${encodeURIComponent(g)}`)
		);
		agentWs = w;
		const finish = () => {
			agentRunning = false;
			if (rfb) rfb.viewOnly = false;
		};
		w.onmessage = (e) => {
			let m: { type: string; text?: string };
			try {
				m = JSON.parse(e.data);
			} catch {
				return;
			}
			if (m.type === 'done') {
				agentLine = m.text || 'done ✓';
				finish();
				w.close();
			} else if (m.type === 'error') {
				agentLine = '⚠ ' + (m.text ?? 'something went wrong');
				finish();
				w.close();
			} else if ((m.type === 'say' || m.type === 'action') && m.text) {
				agentLine = m.text;
			}
		};
		w.onclose = finish;
	}

	function promptKey(e: KeyboardEvent) {
		if (e.key === 'Enter') {
			e.preventDefault();
			e.stopPropagation();
			runAgent();
		}
	}

	// --- lifecycle ---
	function startCountdown() {
		remaining = machine?.expires_at
			? Math.max(0, Math.round((new Date(machine.expires_at).getTime() - Date.now()) / 1000))
			: ttl;
		countdown = setInterval(() => {
			remaining -= 1;
			if (remaining <= 0) stopCountdown();
		}, 1000);
	}
	function stopCountdown() {
		if (countdown) clearInterval(countdown);
		countdown = null;
	}

	async function copyShare() {
		if (!machine) return;
		try {
			await navigator.clipboard.writeText(`${location.origin}/c/${machine.id}`);
		} catch {
			/* ignore */
		}
		shared = true;
		copied = true;
		setTimeout(() => (copied = false), 1600);
	}

	export function close() {
		disposed = true;
		stopCountdown();
		if (onResize) window.removeEventListener('resize', onResize);
		onResize = null;
		for (const s of [tty, agentWs]) {
			try {
				s?.close();
			} catch {
				/* ignore */
			}
		}
		tty = null;
		agentWs = null;
		try {
			rfb?.disconnect();
		} catch {
			/* ignore */
		}
		rfb = null;
		term?.dispose();
		term = null;
		fit = null;
		if (machine && !shared && !machineId) {
			void fetch(`${apiBase}/v1/machines/${machine.id}`, { method: 'DELETE' }).catch(() => {});
		}
		machine = null;
		phase = 'closed';
		onClose?.();
	}
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
	class="relative flex h-full flex-col bg-black font-mono text-[12px]"
	ondragover={(e) => {
		if (phase === 'live') {
			e.preventDefault();
			dragOver = true;
		}
	}}
	ondragleave={() => (dragOver = false)}
	ondrop={onDrop}
>
	{#if dragOver}
		<div
			class="pointer-events-none absolute inset-0 z-20 flex items-center justify-center border-2 border-dashed border-accent/70 bg-black/70 text-[13px] text-ink"
		>
			drop to upload into /root
		</div>
	{/if}
	<!-- status + tabs -->
	<div class="flex items-center justify-between border-b border-line/70 px-3 py-1.5">
		<div class="flex min-w-0 items-center gap-2 text-ink-muted">
			{#if phase === 'booting'}
				<span class="size-1.5 animate-pulse rounded-full bg-ink-subtle"></span>booting your
				computer…
			{:else if phase === 'connecting'}
				<span class="size-1.5 animate-pulse rounded-full bg-ink-subtle"></span>starting the display…
			{:else if phase === 'live'}
				<span class="size-1.5 rounded-full bg-success"></span><span class="truncate text-ink"
					>{machine?.id}</span
				>
			{:else if phase === 'error'}
				<span class="size-1.5 rounded-full bg-danger"></span><span class="truncate text-danger"
					>{error}</span
				>
			{:else}
				<span class="size-1.5 rounded-full bg-ink-faint"></span>computer stopped
			{/if}
		</div>
		<div class="flex items-center gap-1">
			<button
				onclick={() => (tab = 'desktop')}
				class="rounded-[5px] px-2 py-0.5 transition-colors {tab === 'desktop'
					? 'bg-white/10 text-ink'
					: 'text-ink-faint hover:text-ink-muted'}">desktop</button
			>
			<button
				onclick={showTerminal}
				class="rounded-[5px] px-2 py-0.5 transition-colors {tab === 'terminal'
					? 'bg-white/10 text-ink'
					: 'text-ink-faint hover:text-ink-muted'}">terminal</button
			>
		</div>
		<div class="flex items-center gap-3 text-ink-faint">
			{#if phase === 'live'}
				<span class="flex items-center gap-1" title="Open a port running inside this computer">
					<input
						bind:value={previewPort}
						onkeydown={previewKey}
						placeholder="port"
						inputmode="numeric"
						class="w-11 rounded-[4px] border border-line bg-black px-1 py-0.5 text-right text-ink placeholder:text-ink-faint focus:border-white/25 focus:outline-none"
					/>
					<button class="text-ink-subtle transition-colors hover:text-ink" onclick={openPreview}
						>preview ↗</button
					>
				</span>
				<span class="tabular-nums">{remaining}s</span>
				<button class="text-ink-subtle transition-colors hover:text-ink" onclick={copyShare}
					>{copied ? 'copied ✓' : 'share'}</button
				>
			{/if}
			<button class="text-ink-subtle transition-colors hover:text-ink" onclick={close}>✕</button>
		</div>
	</div>

	<!-- main viewport: desktop OR terminal -->
	<div class="relative min-h-0 flex-1">
		<div bind:this={screen} class="h-full w-full" class:hidden={tab !== 'desktop'}></div>
		<div class="h-full w-full overflow-hidden bg-[#0a0a0a] p-2" class:hidden={tab !== 'terminal'}>
			<div bind:this={termHost} class="h-full w-full"></div>
		</div>
		{#if tab === 'desktop' && phase !== 'live' && phase !== 'error'}
			<div
				class="pointer-events-none absolute inset-0 flex items-center justify-center text-ink-subtle"
			>
				{phase === 'booting' ? 'allocating a computer…' : 'painting the screen…'}
			</div>
		{/if}
	</div>

	<!-- file bar: drag-drop upload feedback + download -->
	{#if phase === 'live'}
		<div class="flex items-center gap-2 border-t border-line/70 bg-black px-3 py-1 text-[11px]">
			<span class="min-w-0 flex-1 truncate {fileMsg ? 'text-ink-muted' : 'text-ink-faint'}">
				{fileMsg || 'drag a file in to upload it to /root'}
			</span>
			<input
				bind:value={dlPath}
				onkeydown={(e) => e.key === 'Enter' && downloadFile()}
				placeholder="/root/result.txt"
				class="w-40 rounded-[4px] border border-line bg-black px-1.5 py-0.5 text-ink placeholder:text-ink-faint focus:border-white/25 focus:outline-none"
			/>
			<button class="text-ink-subtle transition-colors hover:text-ink" onclick={downloadFile}
				>download ↓</button
			>
		</div>
	{/if}

	<!-- AI prompt box -->
	<div class="border-t border-line/70 bg-surface px-3 py-2">
		<div class="flex items-center gap-2">
			<span class="font-semibold text-accent">AI</span>
			<input
				bind:value={goal}
				onkeydown={promptKey}
				disabled={agentRunning || phase !== 'live'}
				placeholder="tell your computer what to do — “search Wikipedia for Firecracker and summarize it”"
				class="min-w-0 flex-1 bg-transparent text-ink placeholder:text-ink-faint focus:outline-none disabled:opacity-60"
			/>
			<button
				onclick={runAgent}
				disabled={agentRunning || phase !== 'live' || !goal.trim()}
				class="rounded-geist bg-ink px-2.5 py-1 text-[11px] text-black transition-opacity hover:opacity-90 disabled:opacity-40"
			>
				{agentRunning ? 'working…' : 'run'}
			</button>
		</div>
		{#if agentLine}
			<p class="mt-1.5 flex items-center gap-1.5 text-[11px] text-ink-muted">
				{#if agentRunning}<span class="size-1.5 animate-pulse rounded-full bg-accent"></span>{/if}
				<span class="truncate">{agentLine}</span>
			</p>
		{/if}
	</div>
</div>
