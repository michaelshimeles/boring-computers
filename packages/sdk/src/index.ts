/**
 * @boring/sdk — Effect-native TypeScript client for the boring computers
 * Firecracker microVM API.
 *
 * Every call is an `Effect` with typed errors; the serial console is a `Stream`
 * with `Scope`-based teardown. Relies on the global `fetch` and `WebSocket`
 * (Node 24+ / browsers).
 */

import { Context, Data, Duration, Effect, Layer, Queue, Schedule, Scope, Stream } from 'effect';

// --- wire types -------------------------------------------------------------

export type MachineMode = 'coldboot' | 'snapshot' | 'warm';
export type MachineStatus = 'running';

/** A microVM as returned by the boringd REST API. */
export interface Machine {
	readonly id: string;
	readonly status: MachineStatus;
	readonly mode: MachineMode;
	readonly boot_ms: number;
	readonly template: string;
	readonly created_at: string;
	readonly expires_at: string;
}

export interface CreateMachineOptions {
	readonly template?: string;
	readonly ttlSeconds?: number;
	/** Give the machine internet (cold-boots instead of restoring a snapshot). */
	readonly net?: boolean;
}

export interface BoringClientOptions {
	/** Base URL of boringd. Defaults to `http://localhost:8080`. */
	readonly baseUrl?: string;
	/** Optional bearer token (sent as `Authorization` and `?token=` on WS). */
	readonly token?: string;
}

// --- errors -----------------------------------------------------------------

/** The request never got a response (network error, timeout, bad URL). */
export class RequestError extends Data.TaggedError('RequestError')<{
	readonly method: string;
	readonly path: string;
	readonly cause: unknown;
}> {}

/** The API responded with a non-2xx status. */
export class ResponseError extends Data.TaggedError('ResponseError')<{
	readonly status: number;
	readonly body: string;
}> {}

export type BoringError = RequestError | ResponseError;

// --- serial console ---------------------------------------------------------

/** A live serial-console channel: `output` streams guest bytes, `send` writes stdin. */
export interface TtyChannel {
	readonly output: Stream.Stream<Uint8Array>;
	readonly send: (data: Uint8Array | string) => Effect.Effect<void>;
}

// --- service ----------------------------------------------------------------

export interface BoringClient {
	readonly createMachine: (opts?: CreateMachineOptions) => Effect.Effect<Machine, BoringError>;
	readonly listMachines: Effect.Effect<ReadonlyArray<Machine>, BoringError>;
	readonly getMachine: (id: string) => Effect.Effect<Machine, BoringError>;
	readonly destroyMachine: (id: string) => Effect.Effect<void, BoringError>;
	readonly branchMachine: (id: string) => Effect.Effect<Machine, BoringError>;
	/** Open a serial console. The socket is closed when the enclosing `Scope` closes. */
	readonly connectTty: (id: string) => Effect.Effect<TtyChannel, RequestError, Scope.Scope>;
}

export const BoringClient = Context.GenericTag<BoringClient>('@boring/sdk/BoringClient');

/** A `Layer` providing {@link BoringClient} from static options. */
export const layer = (options: BoringClientOptions = {}): Layer.Layer<BoringClient> =>
	Layer.succeed(BoringClient, make(options));

/** Build a {@link BoringClient} implementation directly (no layer). */
export const make = (options: BoringClientOptions = {}): BoringClient => {
	const baseUrl = (options.baseUrl ?? 'http://localhost:8080').replace(/\/+$/, '');
	const token = options.token;

	const request = <A>(
		method: string,
		path: string,
		body?: unknown
	): Effect.Effect<A, BoringError> =>
		Effect.gen(function* () {
			const headers: Record<string, string> = {};
			if (token !== undefined) headers['Authorization'] = `Bearer ${token}`;
			if (body !== undefined) headers['Content-Type'] = 'application/json';

			const res = yield* Effect.tryPromise({
				try: (signal) =>
					fetch(`${baseUrl}${path}`, {
						method,
						headers,
						body: body !== undefined ? JSON.stringify(body) : undefined,
						signal
					}),
				catch: (cause) => new RequestError({ method, path, cause })
			});

			if (!res.ok) {
				const text = yield* Effect.promise(() => res.text().catch(() => ''));
				return yield* new ResponseError({ status: res.status, body: text });
			}
			if (res.status === 204) return undefined as A;
			const text = yield* Effect.tryPromise({
				try: () => res.text(),
				catch: (cause) => new RequestError({ method, path, cause })
			});
			return (text.length === 0 ? undefined : JSON.parse(text)) as A;
		});

	// Retry transient failures (transport errors + 5xx) up to twice, backing off.
	const retry = Schedule.exponential(Duration.millis(250)).pipe(
		Schedule.intersect(Schedule.recurs(2)),
		Schedule.whileInput(
			(e: BoringError) =>
				e._tag === 'RequestError' || (e._tag === 'ResponseError' && e.status >= 500)
		)
	);

	const connectTty = (id: string): Effect.Effect<TtyChannel, RequestError, Scope.Scope> =>
		Effect.gen(function* () {
			const wsBase = baseUrl.replace(/^http/, 'ws');
			let url = `${wsBase}/v1/machines/${encodeURIComponent(id)}/tty`;
			if (token !== undefined) url += `?token=${encodeURIComponent(token)}`;

			const queue = yield* Queue.unbounded<Uint8Array>();
			const socket = yield* Effect.acquireRelease(
				Effect.async<WebSocket, RequestError>((resume) => {
					const ws = new WebSocket(url);
					ws.binaryType = 'arraybuffer';
					ws.onopen = () => resume(Effect.succeed(ws));
					ws.onerror = () =>
						resume(
							Effect.fail(new RequestError({ method: 'WS', path: url, cause: 'tty socket error' }))
						);
					ws.onmessage = (e) => {
						const bytes = toUint8Array((e as MessageEvent).data);
						if (bytes !== undefined) Effect.runSync(Queue.offer(queue, bytes));
					};
					ws.onclose = () => Effect.runSync(Queue.shutdown(queue));
				}),
				(ws) => Effect.sync(() => ws.close())
			);

			return {
				output: Stream.fromQueue(queue),
				send: (data) =>
					Effect.sync(() =>
						socket.send(typeof data === 'string' ? new TextEncoder().encode(data) : data)
					)
			} satisfies TtyChannel;
		});

	return {
		createMachine: (opts = {}) => {
			const body: { template?: string; ttl_seconds?: number; net?: boolean } = {};
			if (opts.template !== undefined) body.template = opts.template;
			if (opts.ttlSeconds !== undefined) body.ttl_seconds = opts.ttlSeconds;
			if (opts.net !== undefined) body.net = opts.net;
			return request<Machine>('POST', '/v1/machines', body).pipe(Effect.retry(retry));
		},
		listMachines: request<{ machines: ReadonlyArray<Machine> }>('GET', '/v1/machines').pipe(
			Effect.map((r) => r.machines)
		),
		getMachine: (id) => request<Machine>('GET', `/v1/machines/${encodeURIComponent(id)}`),
		destroyMachine: (id) => request<void>('DELETE', `/v1/machines/${encodeURIComponent(id)}`),
		branchMachine: (id) =>
			request<Machine>('POST', `/v1/machines/${encodeURIComponent(id)}/branch`),
		connectTty
	};
};

function toUint8Array(data: unknown): Uint8Array | undefined {
	if (data instanceof ArrayBuffer) return new Uint8Array(data);
	if (ArrayBuffer.isView(data)) {
		const view = data as ArrayBufferView;
		return new Uint8Array(view.buffer, view.byteOffset, view.byteLength);
	}
	if (typeof data === 'string') return new TextEncoder().encode(data);
	return undefined;
}
