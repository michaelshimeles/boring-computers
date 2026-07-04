# @boring/sdk

An [Effect](https://effect.website)-native TypeScript client for the **boring
computers** Firecracker microVM API (`boringd`). Every call is an `Effect` with
typed errors; the serial console is a `Stream` with `Scope`-based teardown. Uses
the global `fetch` / `WebSocket` (Node 24+ / browsers).

> Lives in the monorepo — not published to npm. Build it and import from `dist/`.

## Build

```sh
npm install                     # once, at the repo root
npm run build -w @boring/sdk    # tsc -> dist/
```

## Usage

```ts
import { Effect, Stream } from 'effect';
import { make } from '@boring/sdk';

const boring = make({ baseUrl: 'http://localhost:8080' });

const program = Effect.gen(function* () {
	const vm = yield* boring.createMachine({ template: 'python', ttlSeconds: 300 });

	// Typed errors — no throws. Catch by tag:
	const found = yield* boring
		.getMachine(vm.id)
		.pipe(Effect.catchTag('ResponseError', (e) => Effect.succeed(`http ${e.status}`)));

	// The serial console is a Stream; the socket closes with the Scope.
	yield* Effect.scoped(
		Effect.gen(function* () {
			const tty = yield* boring.connectTty(vm.id);
			yield* tty.send("python3 -c 'print(2 + 2)'\n");
			yield* tty.output.pipe(
				Stream.runForEach((bytes) => Effect.sync(() => process.stdout.write(bytes)))
			);
		})
	);

	yield* boring.destroyMachine(vm.id);
});

Effect.runPromise(program);
```

Prefer dependency injection? Use `layer({ baseUrl })` and the `BoringClient` tag
(`yield* BoringClient`).

### API

- `make({ baseUrl?, token? }): BoringClient` — build a client
- `layer({ baseUrl?, token? })` + `BoringClient` tag — the same, as a `Layer`
- `createMachine(opts?: { template?, ttlSeconds?, net? }): Effect<Machine, BoringError>`
  — retries transient failures internally
- `listMachines: Effect<Machine[], BoringError>`
- `getMachine(id) / branchMachine(id): Effect<Machine, BoringError>`
- `destroyMachine(id): Effect<void, BoringError>`
- `connectTty(id): Effect<TtyChannel, RequestError, Scope>` — `{ output: Stream, send }`

Errors are tagged: `RequestError` (transport) and `ResponseError` (`{ status, body }`).

## Demo

`demo.mjs` boots a `python` VM and drops you into a live shell (destroyed on exit).
Build first, then:

```sh
BORING_URL=http://localhost:8080 node demo.mjs   # Ctrl-] to quit
```
