#!/usr/bin/env node
// MCP server for boring computers. Lets any MCP client (Claude Desktop, Cursor,
// etc.) spin up and drive a real Linux computer: run tasks, take screenshots,
// fork it, expose ports. Wraps the public boringd API — no key required.
//
//   npx @boring/mcp        (or: node index.mjs)
//   env BORING_URL=https://…  to point at a different endpoint

import { Server } from '@modelcontextprotocol/sdk/server/index.js';
import { StdioServerTransport } from '@modelcontextprotocol/sdk/server/stdio.js';
import {
	CallToolRequestSchema,
	ListToolsRequestSchema
} from '@modelcontextprotocol/sdk/types.js';
import { Effect } from 'effect';
import { make } from '@boring/sdk';

const BASE = process.env.BORING_URL || 'https://162-43-188-89.sslip.io';
const WSBASE = BASE.replace(/^http/, 'ws');
const PREVIEW_HOST = new URL(BASE).host;

// The MCP protocol layer is Promise-based; machine ops go through the Effect SDK,
// run to a Promise at this boundary.
const boring = make({ baseUrl: BASE });
const run = (effect) => Effect.runPromise(effect);

// Run a natural-language task via the terminal agent, collecting its narration.
function runTask(id, goal) {
	return new Promise((resolve) => {
		const ws = new WebSocket(`${WSBASE}/v1/machines/${id}/shell-agent?goal=${encodeURIComponent(goal)}`);
		const log = [];
		let preview = null;
		const timer = setTimeout(() => {
			try {
				ws.close();
			} catch {}
			resolve({ log, preview, note: 'timed out' });
		}, 180000);
		ws.onmessage = (e) => {
			let m;
			try {
				m = JSON.parse(e.data);
			} catch {
				return;
			}
			if (m.type === 'preview') preview = m.text;
			else if (m.type === 'action') log.push('$ ' + m.text);
			else if (m.type === 'say' || m.type === 'done') log.push(m.text);
			if (m.type === 'done' || m.type === 'error') {
				clearTimeout(timer);
				try {
					ws.close();
				} catch {}
				resolve({ log: log.filter(Boolean), preview });
			}
		};
		ws.onerror = () => {
			clearTimeout(timer);
			resolve({ log, preview, note: 'connection error' });
		};
	});
}

const TOOLS = [
	{
		name: 'launch_computer',
		description:
			'Boot a fresh computer (a Firecracker microVM). "desktop" = a full Linux GUI with a browser + coding agents; "python" = a fast headless shell. Returns the machine id you pass to the other tools.',
		inputSchema: {
			type: 'object',
			properties: {
				template: { type: 'string', enum: ['desktop', 'python'], default: 'desktop' },
				internet: { type: 'boolean', description: 'give the shell internet (desktops always have it)', default: true },
				volume: { type: 'string', description: 'optional volume id to restore into /root on boot' },
				ttl_seconds: { type: 'number', description: 'auto-destroy after this many seconds (15–900)', default: 600 }
			}
		}
	},
	{
		name: 'run_task',
		description:
			'Give the computer a natural-language task; an agent writes and runs commands to do it (installing packages, building + serving apps, etc.). If it starts a web server it returns a live preview URL.',
		inputSchema: {
			type: 'object',
			properties: {
				id: { type: 'string' },
				task: { type: 'string', description: 'e.g. "build a snake game in python and serve it"' }
			},
			required: ['id', 'task']
		}
	},
	{
		name: 'screenshot',
		description: 'Capture a PNG screenshot of a desktop computer.',
		inputSchema: { type: 'object', properties: { id: { type: 'string' } }, required: ['id'] }
	},
	{
		name: 'preview_url',
		description: 'Get the public HTTPS URL for a port running inside a computer.',
		inputSchema: {
			type: 'object',
			properties: { id: { type: 'string' }, port: { type: 'number' } },
			required: ['id', 'port']
		}
	},
	{
		name: 'fork_computer',
		description: 'Clone a running computer (its exact live state) into a new one. Returns the fork id.',
		inputSchema: { type: 'object', properties: { id: { type: 'string' } }, required: ['id'] }
	},
	{
		name: 'list_computers',
		description: 'List the currently running computers.',
		inputSchema: { type: 'object', properties: {} }
	},
	{
		name: 'stop_computer',
		description: 'Destroy a computer now.',
		inputSchema: { type: 'object', properties: { id: { type: 'string' } }, required: ['id'] }
	},
	{
		name: 'create_volume',
		description:
			'Create a persistent volume — storage that outlives a computer. Save a computer into it, then restore it into a fresh computer later (pass its id as launch_computer volume).',
		inputSchema: {
			type: 'object',
			properties: { ttl_seconds: { type: 'number', description: 'how long the volume lives' } }
		}
	},
	{
		name: 'save_computer',
		description: "Save a computer's /root into a volume, so its work survives the self-destruct.",
		inputSchema: {
			type: 'object',
			properties: { id: { type: 'string' }, volume: { type: 'string' } },
			required: ['id', 'volume']
		}
	}
];

function text(s) {
	return { content: [{ type: 'text', text: s }] };
}

async function dispatch(name, a) {
	switch (name) {
		case 'launch_computer': {
			const m = await run(
				boring.createMachine({
					template: a.template || 'desktop',
					net: a.internet !== false,
					ttlSeconds: a.ttl_seconds || 600,
					volume: a.volume || undefined
				})
			);
			return text(
				`Launched ${m.template} computer ${m.id} (${m.mode}, ${m.boot_ms}ms). It self-destructs at ${m.expires_at}. Use run_task with id "${m.id}".`
			);
		}
		case 'create_volume': {
			const v = await run(boring.createVolume(a.ttl_seconds || undefined));
			return text(
				`Created volume ${v.id} (${v.quota_mb}MB). Save a computer into it with save_computer, then restore it by passing volume "${v.id}" to launch_computer.`
			);
		}
		case 'save_computer': {
			await run(boring.saveMachine(a.id, a.volume));
			return text(`Saved ${a.id} into volume ${a.volume}. Launch a new computer with that volume to restore it.`);
		}
		case 'run_task': {
			const { log, preview, note } = await runTask(a.id, a.task);
			let out = log.join('\n') || '(no output)';
			if (preview) out += `\n\nLive preview: ${preview}`;
			if (note) out += `\n(${note})`;
			return text(out);
		}
		case 'screenshot': {
			const res = await fetch(`${BASE}/v1/machines/${a.id}/screenshot`);
			if (!res.ok) throw new Error(`screenshot failed (${res.status})`);
			const buf = Buffer.from(await res.arrayBuffer());
			return { content: [{ type: 'image', data: buf.toString('base64'), mimeType: 'image/png' }] };
		}
		case 'preview_url':
			return text(`https://${a.id}--${a.port}.${PREVIEW_HOST}/`);
		case 'fork_computer': {
			const f = await run(boring.branchMachine(a.id));
			return text(`Forked → ${f.id} (${f.mode}, ${f.boot_ms}ms). A live clone of ${a.id}.`);
		}
		case 'list_computers': {
			const arr = await run(boring.listMachines);
			return text(arr.length ? JSON.stringify(arr, null, 2) : 'No computers running.');
		}
		case 'stop_computer':
			await run(boring.destroyMachine(a.id));
			return text(`Stopped ${a.id}.`);
		default:
			throw new Error(`unknown tool ${name}`);
	}
}

const server = new Server(
	{ name: 'boring-computers', version: '0.1.0' },
	{ capabilities: { tools: {} } }
);
server.setRequestHandler(ListToolsRequestSchema, async () => ({ tools: TOOLS }));
server.setRequestHandler(CallToolRequestSchema, async (req) => {
	try {
		return await dispatch(req.params.name, req.params.arguments || {});
	} catch (e) {
		return { content: [{ type: 'text', text: 'Error: ' + (e?.message || String(e)) }], isError: true };
	}
});

await server.connect(new StdioServerTransport());
console.error('boring-computers MCP server ready (endpoint: ' + BASE + ')');
