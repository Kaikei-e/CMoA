import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';

/**
 * A stand-in for one llama-server. It answers the three routes the monitor
 * asks for and nothing else, and it can be stopped and started again while the
 * suite runs so a test can watch a lane go unreachable.
 */

export type FleetMode = 'idle' | 'prefill' | 'generating';

export interface FakeServerSpec {
	port: number;
	model: string;
	/** Number of slots the server reports; the judge has several. */
	slots?: number;
	mode?: FleetMode;
	/** A `serve` stand-in answers `/v1/models` only. */
	serveOnly?: boolean;
}

function slotDocument(spec: Required<Pick<FakeServerSpec, 'slots' | 'mode'>>): unknown[] {
	const out: unknown[] = [];
	for (let id = 0; id < spec.slots; id += 1) {
		const busy = spec.mode !== 'idle' && id === 0;
		out.push({
			id,
			n_ctx: 8192,
			is_processing: busy,
			n_prompt_tokens: 412,
			n_prompt_tokens_processed: busy ? (spec.mode === 'prefill' ? 300 : 412) : 0,
			next_token: [
				{
					has_next_token: busy,
					n_remain: busy ? 375 : -1,
					n_decoded: busy && spec.mode === 'generating' ? 137 : 0
				}
			]
		});
	}
	return out;
}

export class FakeServer {
	readonly spec: Required<FakeServerSpec>;
	#server: Server | null = null;

	constructor(spec: FakeServerSpec) {
		this.spec = {
			slots: 1,
			mode: 'idle',
			serveOnly: false,
			...spec
		};
	}

	get port(): number {
		return this.spec.port;
	}

	get listening(): boolean {
		return this.#server !== null;
	}

	async start(): Promise<void> {
		if (this.#server) return;
		const server = createServer((request, response) => {
			const path = (request.url ?? '/').split('?')[0];
			const send = (status: number, body: unknown, type = 'application/json') => {
				const text = typeof body === 'string' ? body : JSON.stringify(body);
				response.writeHead(status, { 'content-type': type, 'content-length': Buffer.byteLength(text) });
				response.end(text);
			};
			if (path === '/v1/models') {
				send(200, { object: 'list', data: [{ id: this.spec.model, object: 'model' }] });
				return;
			}
			if (this.spec.serveOnly) {
				send(404, { error: 'not found' });
				return;
			}
			if (path === '/slots') {
				send(200, slotDocument(this.spec));
				return;
			}
			if (path === '/metrics') {
				send(
					200,
					'llamacpp:requests_processing 0\nllamacpp:tokens_predicted_total 0\n',
					'text/plain'
				);
				return;
			}
			send(404, { error: 'not found' });
		});
		await new Promise<void>((resolve, reject) => {
			server.once('error', reject);
			server.listen(this.spec.port, '127.0.0.1', () => {
				server.off('error', reject);
				resolve();
			});
		});
		this.#server = server;
	}

	async stop(): Promise<void> {
		const server = this.#server;
		if (!server) return;
		this.#server = null;
		await new Promise<void>((resolve) => {
			server.closeAllConnections?.();
			server.close(() => resolve());
		});
	}
}

/** The ports the suite uses. They are deliberately far from a real fleet's. */
export const FLEET_PORTS = {
	granite: 8481,
	qwen: 8482,
	gemma: 8483,
	judge: 8490,
	serve: 8495,
	/** The control endpoint tests use to stop and start a member. */
	control: 8499
} as const;

export const FLEET_SPECS: FakeServerSpec[] = [
	{ port: FLEET_PORTS.granite, model: 'granite-4.2-8b' },
	{ port: FLEET_PORTS.qwen, model: 'qwen3.5-9b' },
	{ port: FLEET_PORTS.gemma, model: 'gemma-4-12b' },
	{ port: FLEET_PORTS.judge, model: 'gpt-oss-20b', slots: 6 },
	{ port: FLEET_PORTS.serve, model: 'cmoa', serveOnly: true }
];

/**
 * The whole fake fleet plus a control server. The fleet lives in the Playwright
 * process for the length of the run, so a worker cannot reach into it directly:
 * `POST /stop/<port>` and `POST /start/<port>` on the control port are how a
 * test takes a server away and gives it back.
 */
export class FakeFleet {
	readonly servers: FakeServer[];
	#control: Server | null = null;

	constructor(specs: FakeServerSpec[] = FLEET_SPECS) {
		this.servers = specs.map((spec) => new FakeServer(spec));
	}

	async start(controlPort = FLEET_PORTS.control): Promise<void> {
		for (const server of this.servers) await server.start();
		const control = createServer((request, response) => {
			const [, action, port] = (request.url ?? '').split('/');
			const target = this.servers.find((server) => server.port === Number(port));
			const finish = (status: number, body: unknown) => {
				const text = JSON.stringify(body);
				response.writeHead(status, { 'content-type': 'application/json' });
				response.end(text);
			};
			if (!target) {
				finish(404, { error: `no fake server on ${port}` });
				return;
			}
			const done = () => finish(200, { port: target.port, listening: target.listening });
			if (action === 'stop') void target.stop().then(done);
			else if (action === 'start') void target.start().then(done);
			else finish(400, { error: `unknown action ${action}` });
		});
		await new Promise<void>((resolve) => control.listen(controlPort, '127.0.0.1', () => resolve()));
		this.#control = control;
	}

	async stop(): Promise<void> {
		for (const server of this.servers) await server.stop();
		const control = this.#control;
		this.#control = null;
		if (control) {
			await new Promise<void>((resolve) => {
				control.closeAllConnections?.();
				control.close(() => resolve());
			});
		}
	}

	/** The port a listening control server actually bound, for a caller that asked for 0. */
	get controlPort(): number {
		const address = this.#control?.address() as AddressInfo | null;
		return address?.port ?? FLEET_PORTS.control;
	}
}
