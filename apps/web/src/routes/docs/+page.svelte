<script lang="ts">
	import { resolve } from '$app/paths';
	const API = 'https://162-43-188-89.sslip.io';
</script>

<svelte:head>
	<title>Docs · boring computers</title>
	<meta
		name="description"
		content="Boot a Firecracker microVM in milliseconds over a plain REST + WebSocket API."
	/>
</svelte:head>

{#snippet code(text: string)}
	<pre
		class="overflow-x-auto rounded-geist border border-line bg-surface p-4 font-mono text-[12px] leading-relaxed text-ink-muted"><code
			>{text}</code
		></pre>
{/snippet}

<div class="mx-auto max-w-2xl px-5 pt-24 pb-24">
	<h1 class="text-[28px] font-semibold tracking-[-0.03em] text-ink">Docs</h1>
	<p class="mt-3 leading-relaxed text-ink-muted">
		A computer is one HTTP call away. <span class="text-ink">boringd</span> boots a Firecracker
		microVM — jailed, resource-capped, network-isolated — and hands you a serial console, a VNC
		display, or an AI that drives it. Snapshot-restore means a shell is ready in
		<span class="text-ink">~3&nbsp;ms</span>. Every machine self-destructs when its TTL expires.
	</p>

	<h2 class="mt-12 text-[15px] font-semibold text-ink">Base URL</h2>
	<p class="mt-2 text-[13px] leading-relaxed text-ink-muted">
		The public endpoint is token-less (rate-limited per IP). Point everything at:
	</p>
	<div class="mt-3">{@render code(API)}</div>

	<h2 class="mt-12 text-[15px] font-semibold text-ink">Quickstart</h2>
	<p class="mt-2 text-[13px] leading-relaxed text-ink-muted">Boot a machine and read it back:</p>
	<div class="mt-3">
		{@render code(`# boot a shell (python3 + node), 60s TTL
curl -s -X POST ${API}/v1/machines \\
  -H 'content-type: application/json' \\
  -d '{"template":"python","ttl_seconds":60}'

# → {"id":"m-1a2b3c4d","mode":"snapshot","boot_ms":3,
#    "template":"python","expires_at":"..."}`)}
	</div>

	<h2 class="mt-12 text-[15px] font-semibold text-ink">REST</h2>
	<div class="mt-3 overflow-x-auto rounded-geist border border-line">
		<table class="w-full border-collapse font-mono text-[12px]">
			<tbody class="text-ink-muted">
				{#each [['POST', '/v1/machines', 'Boot a machine. Body: {template, ttl_seconds}'], ['GET', '/v1/machines', 'List running machines'], ['GET', '/v1/machines/{id}', 'Fetch one machine'], ['DELETE', '/v1/machines/{id}', 'Destroy a machine now'], ['POST', '/v1/machines/{id}/branch', 'Fork a machine from its snapshot'], ['GET', '/healthz', 'Liveness + running count']] as row (row[1] + row[0])}
					<tr class="border-b border-line last:border-0">
						<td class="w-16 px-3 py-2 align-top text-accent">{row[0]}</td>
						<td class="px-3 py-2 align-top whitespace-nowrap text-ink">{row[1]}</td>
						<td class="px-3 py-2 align-top text-ink-faint">{row[2]}</td>
					</tr>
				{/each}
			</tbody>
		</table>
	</div>
	<p class="mt-3 text-[13px] leading-relaxed text-ink-muted">
		Templates: <span class="text-ink">python</span> (headless shell, snapshot, ~3&nbsp;ms) and
		<span class="text-ink">desktop</span> (GUI over VNC). <code class="text-ink">ttl_seconds</code>
		is clamped to 15–900. Pass <code class="text-ink">"net": true</code> to give the machine
		internet (cold-boots instead of snapshot; <code class="text-ink">pip</code>/<code
			class="text-ink">npm</code
		>/<code class="text-ink">apk</code> install work). Guests are NAT'd and egress-firewalled.
	</p>

	<h2 class="mt-12 text-[15px] font-semibold text-ink">WebSockets</h2>
	<p class="mt-2 text-[13px] leading-relaxed text-ink-muted">
		Interactive channels upgrade to WebSocket (binary frames):
	</p>
	<div class="mt-3 overflow-x-auto rounded-geist border border-line">
		<table class="w-full border-collapse font-mono text-[12px]">
			<tbody class="text-ink-muted">
				{#each [['/v1/machines/{id}/tty', 'Serial console — bytes ⇄ the guest /dev/ttyS0'], ['/v1/machines/{id}/vnc', 'RFB/VNC framebuffer for desktop machines'], ['/v1/machines/{id}/agent?goal=…', 'Run a computer-use agent; streams narration JSON']] as row (row[0])}
					<tr class="border-b border-line last:border-0">
						<td class="px-3 py-2 align-top whitespace-nowrap text-ink">{row[0]}</td>
						<td class="px-3 py-2 align-top text-ink-faint">{row[1]}</td>
					</tr>
				{/each}
			</tbody>
		</table>
	</div>

	<h2 class="mt-12 text-[15px] font-semibold text-ink">TypeScript client</h2>
	<p class="mt-2 text-[13px] leading-relaxed text-ink-muted">
		An <a
			href="https://effect.website"
			target="_blank"
			rel="noopener"
			class="text-accent hover:underline">Effect</a
		>-native client lives in the repo at <code class="text-ink">packages/sdk</code> (not on npm) — typed
		errors, a streaming serial console, retries built in:
	</p>
	<div class="mt-3">
		{@render code(`import { Effect, Stream } from 'effect';
import { make } from '@boring/sdk';

const boring = make({ baseUrl: '${API}' });

Effect.runPromise(
  Effect.gen(function* () {
    const vm = yield* boring.createMachine({ template: 'python', ttlSeconds: 60 });
    console.log(vm.id, vm.mode, \`\${vm.boot_ms}ms\`);

    // the serial console is a Stream; the socket closes with the Scope
    yield* Effect.scoped(
      Effect.gen(function* () {
        const tty = yield* boring.connectTty(vm.id);
        yield* tty.send('print("hello from a microVM")\\n');
        yield* tty.output.pipe(Stream.runForEach((b) => Effect.sync(() => process.stdout.write(b))));
      })
    );

    yield* boring.destroyMachine(vm.id);
  })
);`)}
	</div>
	<p class="mt-3 text-[13px] leading-relaxed text-ink-muted">
		Also: <code class="text-ink">listMachines</code>,
		<code class="text-ink">getMachine(id)</code>,
		<code class="text-ink">branchMachine(id)</code>. Errors are tagged (<code class="text-ink"
			>RequestError</code
		>, <code class="text-ink">ResponseError</code>).
	</p>

	<h2 class="mt-12 text-[15px] font-semibold text-ink">MCP server</h2>
	<p class="mt-2 text-[13px] leading-relaxed text-ink-muted">
		Let any AI — Claude Desktop, Cursor, … — spin up and drive a computer over the
		<a
			href="https://modelcontextprotocol.io"
			target="_blank"
			rel="noopener"
			class="text-accent hover:underline">Model Context Protocol</a
		>. It lives in the repo at <code class="text-ink">packages/mcp</code> (not on npm yet); run it from
		source and point your MCP client at it:
	</p>
	<div class="mt-3">
		{@render code(`{
  "mcpServers": {
    "boring-computers": { "command": "node", "args": ["/path/to/packages/mcp/index.mjs"] }
  }
}`)}
	</div>
	<p class="mt-3 text-[13px] leading-relaxed text-ink-muted">
		Tools: <code class="text-ink">launch_computer</code>,
		<code class="text-ink">run_task</code> (plain-English task → an agent writes + runs the code,
		returns a live URL), <code class="text-ink">screenshot</code>,
		<code class="text-ink">preview_url</code>, <code class="text-ink">fork_computer</code>,
		<code class="text-ink">stop_computer</code>.
	</p>

	<div class="mt-16 border-t border-line pt-6">
		<a
			href={resolve('/')}
			class="font-mono text-[12px] text-ink-subtle transition-colors hover:text-ink"
			>← back to boring computers</a
		>
	</div>
</div>
