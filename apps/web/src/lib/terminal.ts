import { wsUrl } from '$lib/boring';

export interface TerminalHandle {
	// eslint-disable-next-line @typescript-eslint/no-explicit-any
	term: any;
	// eslint-disable-next-line @typescript-eslint/no-explicit-any
	fit: any;
	ws: WebSocket;
	cleanup: () => void;
}

export interface TerminalOptions {
	host: HTMLDivElement;
	machineId: string;
	fontSize?: number;
	theme?: Record<string, string>;
	bannerText: string;
	onClose?: () => void;
}

/**
 * Set up an xterm.js terminal connected to a machine's serial console via
 * WebSocket, including the copy-selection key handler and resize listener.
 * Returns handles for cleanup.
 */
export async function setupTerminal(opts: TerminalOptions): Promise<TerminalHandle> {
	const { Terminal } = await import('@xterm/xterm');
	const { FitAddon } = await import('@xterm/addon-fit');

	const term = new Terminal({
		fontFamily: "'Geist Mono', ui-monospace, monospace",
		fontSize: opts.fontSize ?? 13,
		cursorBlink: true,
		theme: opts.theme ?? {
			background: '#0a0a0a',
			foreground: '#ededed',
			cursor: '#ededed',
			selectionBackground: '#003674',
			black: '#000000',
			green: '#00ca50',
			brightBlack: '#7d7d7d',
			white: '#ededed'
		}
	});
	const fit = new FitAddon();
	term.loadAddon(fit);
	term.open(opts.host);
	fit.fit();

	const onResize = () => fit.fit();
	window.addEventListener('resize', onResize);

	const ws = new WebSocket(wsUrl(`/v1/machines/${opts.machineId}/tty`));
	ws.binaryType = 'arraybuffer';
	const enc = new TextEncoder();

	ws.onmessage = (e) => {
		if (e.data instanceof ArrayBuffer) term.write(new Uint8Array(e.data));
		else term.write(e.data);
	};
	ws.onclose = () => opts.onClose?.();

	term.onData((d: string) => ws.readyState === WebSocket.OPEN && ws.send(enc.encode(d)));

	// Copy selection: Cmd+C (mac) or Ctrl+Shift+C; bare Ctrl+C stays SIGINT.
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

	// Clear boot scrollback and show the banner.
	setTimeout(() => {
		if (!term) return;
		term.reset();
		term.write(opts.bannerText);
		if (ws.readyState === WebSocket.OPEN) ws.send(enc.encode('\n'));
	}, 500);

	term.focus();

	function cleanup() {
		window.removeEventListener('resize', onResize);
		try {
			ws.close();
		} catch {
			/* ignore */
		}
		term.dispose();
	}

	return { term, fit, ws, cleanup };
}
