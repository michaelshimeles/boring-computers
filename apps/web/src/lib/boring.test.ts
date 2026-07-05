import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
	apiBase,
	branchMachine,
	createMachine,
	createVolume,
	fleetCount,
	getMachine,
	previewUrl,
	saveMachine,
	wsUrl
} from './boring';

// Build a minimal Response-like object for the mocked fetch. Only the bits the
// code under test touches (ok, status, json) are implemented.
function jsonResponse(body: unknown, init: { ok?: boolean; status?: number } = {}): Response {
	const status = init.status ?? 200;
	const ok = init.ok ?? (status >= 200 && status < 300);
	return {
		ok,
		status,
		json: async () => body
	} as unknown as Response;
}

let fetchMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
	fetchMock = vi.fn();
	vi.stubGlobal('fetch', fetchMock);
});

afterEach(() => {
	vi.unstubAllGlobals();
	vi.useRealTimers();
});

describe('URL helpers', () => {
	it('apiBase defaults to the dev proxy path', () => {
		expect(apiBase).toBe('/boring');
	});

	it('previewUrl builds the path-based proxy URL through the api base', () => {
		expect(previewUrl('abc123', 3000)).toBe('/boring/v1/machines/abc123/web/3000/');
	});

	it('wsUrl derives ws proto and host from location in dev', () => {
		vi.stubGlobal('location', { protocol: 'http:', host: 'example.test:5173' });
		expect(wsUrl('/v1/machines/x/tty')).toBe('ws://example.test:5173/boring/v1/machines/x/tty');
	});

	it('wsUrl uses wss when the page is served over https', () => {
		vi.stubGlobal('location', { protocol: 'https:', host: 'example.test' });
		expect(wsUrl('/path')).toBe('wss://example.test/boring/path');
	});
});

describe('createVolume', () => {
	it('POSTs the ttl and returns the created id', async () => {
		fetchMock.mockResolvedValueOnce(jsonResponse({ id: 'vol-1' }));

		const out = await createVolume(123);

		expect(out).toEqual({ id: 'vol-1' });
		const [url, opts] = fetchMock.mock.calls[0];
		expect(url).toBe('/boring/v1/volumes');
		expect(opts.method).toBe('POST');
		expect(JSON.parse(opts.body)).toEqual({ ttl_seconds: 123 });
	});

	it('defaults the ttl to one week', async () => {
		fetchMock.mockResolvedValueOnce(jsonResponse({ id: 'vol-2' }));

		await createVolume();

		expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual({ ttl_seconds: 604800 });
	});

	it('throws a friendly message when storage is unavailable', async () => {
		fetchMock.mockResolvedValueOnce(jsonResponse({}, { status: 503 }));

		await expect(createVolume()).rejects.toThrow('storage is unavailable right now');
	});
});

describe('saveMachine', () => {
	it('POSTs to the save endpoint with the volume query param', async () => {
		fetchMock.mockResolvedValueOnce(jsonResponse({}, { status: 200 }));

		await saveMachine('m1', 'vol/one');

		const [url, opts] = fetchMock.mock.calls[0];
		expect(url).toBe('/boring/v1/machines/m1/save?volume=vol%2Fone');
		expect(opts.method).toBe('POST');
	});

	it('surfaces the server-provided error message', async () => {
		fetchMock.mockResolvedValueOnce(jsonResponse({ error: 'disk full' }, { status: 500 }));

		await expect(saveMachine('m1', 'v1')).rejects.toThrow('disk full');
	});

	it('falls back to a generic message when the error body is not JSON', async () => {
		fetchMock.mockResolvedValueOnce({
			ok: false,
			status: 500,
			json: async () => {
				throw new Error('not json');
			}
		} as unknown as Response);

		await expect(saveMachine('m1', 'v1')).rejects.toThrow('save failed');
	});
});

describe('branchMachine', () => {
	it('returns the forked machine on success', async () => {
		const machine = { id: 'm2', mode: 'warm', boot_ms: 5 };
		fetchMock.mockResolvedValueOnce(jsonResponse(machine));

		await expect(branchMachine('m1')).resolves.toEqual(machine);
		expect(fetchMock.mock.calls[0][0]).toBe('/boring/v1/machines/m1/branch');
	});

	it('includes the status code when no error body is present', async () => {
		fetchMock.mockResolvedValueOnce(jsonResponse({}, { status: 502 }));

		await expect(branchMachine('m1')).rejects.toThrow('fork failed (502)');
	});
});

describe('getMachine', () => {
	it('encodes the id and returns the machine', async () => {
		const machine = { id: 'a b', mode: 'snapshot', boot_ms: 3 };
		fetchMock.mockResolvedValueOnce(jsonResponse(machine));

		await expect(getMachine('a b')).resolves.toEqual(machine);
		expect(fetchMock.mock.calls[0][0]).toBe('/boring/v1/machines/a%20b');
	});

	it('reports expiry on a 404', async () => {
		fetchMock.mockResolvedValueOnce(jsonResponse({}, { status: 404 }));

		await expect(getMachine('gone')).rejects.toThrow('this computer has expired');
	});

	it('reports the status code on other errors', async () => {
		fetchMock.mockResolvedValueOnce(jsonResponse({}, { status: 500 }));

		await expect(getMachine('x')).rejects.toThrow('the datacenter returned 500');
	});
});

describe('fleetCount', () => {
	it('returns the machine count from /healthz', async () => {
		fetchMock.mockResolvedValueOnce(jsonResponse({ machines: 42 }));

		await expect(fleetCount()).resolves.toBe(42);
		expect(fetchMock.mock.calls[0][0]).toBe('/boring/healthz');
	});

	it('returns 0 when the count is missing or not a number', async () => {
		fetchMock.mockResolvedValueOnce(jsonResponse({ machines: 'lots' }));

		await expect(fleetCount()).resolves.toBe(0);
	});

	it('returns 0 when the request throws', async () => {
		fetchMock.mockRejectedValueOnce(new Error('offline'));

		await expect(fleetCount()).resolves.toBe(0);
	});
});

describe('createMachine', () => {
	it('POSTs the template/ttl/net/volume body and returns the machine', async () => {
		const machine = { id: 'm1', mode: 'coldboot', boot_ms: 10 };
		fetchMock.mockResolvedValueOnce(jsonResponse(machine));

		await expect(createMachine('python', 60, true, 'vol-1')).resolves.toEqual(machine);

		const [url, opts] = fetchMock.mock.calls[0];
		expect(url).toBe('/boring/v1/machines');
		expect(JSON.parse(opts.body)).toEqual({
			template: 'python',
			ttl_seconds: 60,
			net: true,
			volume: 'vol-1'
		});
	});

	it('fails fast on 429 without retrying', async () => {
		fetchMock.mockResolvedValueOnce(jsonResponse({}, { status: 429 }));

		await expect(createMachine('python', 60)).rejects.toThrow(
			'a lot of people are trying this right now — wait a few seconds and retry'
		);
		expect(fetchMock).toHaveBeenCalledTimes(1);
	});

	it('fails fast on 401', async () => {
		fetchMock.mockResolvedValueOnce(jsonResponse({}, { status: 401 }));

		await expect(createMachine('python', 60)).rejects.toThrow(
			'the datacenter rejected the request'
		);
		expect(fetchMock).toHaveBeenCalledTimes(1);
	});

	it('fails fast on other 4xx with the status code', async () => {
		fetchMock.mockResolvedValueOnce(jsonResponse({}, { status: 400 }));

		await expect(createMachine('python', 60)).rejects.toThrow('the datacenter returned 400');
		expect(fetchMock).toHaveBeenCalledTimes(1);
	});

	it('retries on 5xx and eventually succeeds', async () => {
		vi.useFakeTimers();
		const machine = { id: 'm9', mode: 'coldboot', boot_ms: 8 };
		fetchMock
			.mockResolvedValueOnce(jsonResponse({}, { status: 500 }))
			.mockResolvedValueOnce(jsonResponse(machine));

		const promise = createMachine('python', 60);
		await vi.runAllTimersAsync();

		await expect(promise).resolves.toEqual(machine);
		expect(fetchMock).toHaveBeenCalledTimes(2);
	});

	it('retries network errors and throws a friendly message after exhausting attempts', async () => {
		vi.useFakeTimers();
		fetchMock.mockRejectedValue(new Error('network down'));

		const promise = createMachine('python', 60);
		const assertion = expect(promise).rejects.toThrow("couldn't reach the datacenter");
		await vi.runAllTimersAsync();
		await assertion;

		expect(fetchMock).toHaveBeenCalledTimes(3);
	});
});
