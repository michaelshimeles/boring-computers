import { wsUrl } from '$lib/boring';

export interface AgentMessage {
	type: string;
	text?: string;
}

export interface AgentCallbacks {
	onSay?: (text: string) => void;
	onAction?: (text: string) => void;
	onPreview?: (url: string) => void;
	onDone?: (text: string) => void;
	onError?: (text: string) => void;
	onClose?: () => void;
}

/**
 * Open a WebSocket to a machine's agent endpoint and dispatch parsed messages
 * to callbacks. Returns the WebSocket for external lifecycle management.
 */
export function connectAgent(
	machineId: string,
	path: string,
	callbacks: AgentCallbacks
): WebSocket {
	const ws = new WebSocket(wsUrl(path));

	ws.onmessage = (e) => {
		let m: AgentMessage;
		try {
			m = JSON.parse(e.data);
		} catch {
			return;
		}
		switch (m.type) {
			case 'done':
				callbacks.onDone?.(m.text || 'done');
				ws.close();
				break;
			case 'error':
				callbacks.onError?.(m.text || 'something went wrong');
				ws.close();
				break;
			case 'say':
				if (m.text) callbacks.onSay?.(m.text);
				break;
			case 'action':
				if (m.text) callbacks.onAction?.(m.text);
				break;
			case 'preview':
				if (m.text) callbacks.onPreview?.(m.text);
				break;
		}
	};

	ws.onclose = () => callbacks.onClose?.();

	return ws;
}
