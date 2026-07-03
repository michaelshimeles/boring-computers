import { env } from '$env/dynamic/public';

// In production, set PUBLIC_BORING_URL to the public boringd endpoint
// (e.g. https://162-43-188-89.sslip.io) so the browser talks to it directly.
// In dev it's unset and requests go through the Vite `/boring` proxy, which
// injects the token over the SSH tunnel to the box.
const PUB = env.PUBLIC_BORING_URL ?? '';

/** Base for REST calls: the public endpoint in prod, the `/boring` proxy in dev. */
export const apiBase = PUB || '/boring';

/** Build a ws(s):// URL for a boringd WebSocket path (e.g. /v1/machines/ID/tty). */
export function wsUrl(path: string): string {
	if (PUB) return PUB.replace(/^http/, 'ws') + path;
	const proto = location.protocol === 'https:' ? 'wss' : 'ws';
	return `${proto}://${location.host}/boring${path}`;
}

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

/**
 * Create a machine, retrying transient failures (network errors, timeouts, 5xx)
 * up to a few times with backoff. Client errors (401/404/429) fail fast with a
 * friendly message. Returns the parsed machine, or throws an Error whose message
 * is safe to show a visitor.
 */
export async function createMachine(
	template: string,
	ttlSeconds: number,
	net = false
): Promise<Machine> {
	const attempts = 3;
	let last = 'the datacenter is busy — try again in a moment';
	for (let i = 0; i < attempts; i++) {
		let res: Response | null = null;
		try {
			const ctrl = new AbortController();
			// A connected machine cold-boots (~a few seconds), so allow longer.
			const timer = setTimeout(() => ctrl.abort(), net ? 20000 : 12000);
			try {
				res = await fetch(`${apiBase}/v1/machines`, {
					method: 'POST',
					headers: { 'content-type': 'application/json' },
					body: JSON.stringify({ template, ttl_seconds: ttlSeconds, net }),
					signal: ctrl.signal
				});
			} finally {
				clearTimeout(timer);
			}
		} catch {
			last = "couldn't reach the datacenter"; // network/timeout → retryable
		}
		if (res) {
			if (res.ok) return (await res.json()) as Machine;
			if (res.status === 429)
				throw new Error('a lot of people are trying this right now — wait a few seconds and retry');
			if (res.status === 401) throw new Error('the datacenter rejected the request');
			if (res.status < 500) throw new Error(`the datacenter returned ${res.status}`);
			last = `the datacenter is busy (${res.status})`; // 5xx → retryable
		}
		if (i < attempts - 1) await sleep(500 * (i + 1));
	}
	throw new Error(last);
}

export type Machine = {
	id: string;
	mode: string;
	boot_ms: number;
	expires_at?: string;
	display?: boolean;
};

/** Fetch an existing machine by id (for reconnecting to a shared session). */
export async function getMachine(id: string): Promise<Machine> {
	const res = await fetch(`${apiBase}/v1/machines/${encodeURIComponent(id)}`);
	if (res.status === 404) throw new Error('this computer has expired');
	if (!res.ok) throw new Error(`the datacenter returned ${res.status}`);
	return (await res.json()) as Machine;
}

/** How many computers are running right now (from /healthz). 0 on any error. */
export async function fleetCount(): Promise<number> {
	try {
		const res = await fetch(`${apiBase}/healthz`);
		const j = await res.json();
		return typeof j?.machines === 'number' ? j.machines : 0;
	} catch {
		return 0;
	}
}
