import type {
	FleetSample,
	FleetState,
	MonitorConfig,
	ProbeMode,
	ServerReading
} from '$lib/types';
import { viaGateway } from './config';

/** watch.sh gives every sampler `curl -m 0.4`; a slow server must not stall a tick. */
export const PROBE_TIMEOUT_MS = 400;

/** The id the judge server is filed under, so it can share the proposer machinery. */
export const JUDGE_ID = '__judge__';

const UNREACHABLE: ServerReading = {
	reachable: false,
	processing: 0,
	slotCount: 0,
	promptTokens: 0,
	promptProcessed: 0,
	decoded: 0,
	slots: [],
	cumulative: false
};

function toInt(value: unknown): number {
	const n = typeof value === 'number' ? value : Number(value);
	return Number.isFinite(n) ? Math.trunc(n) : 0;
}

/**
 * The decoded count of one slot: `next_token` is an object on some builds and a
 * list on others, and the largest `n_decoded` in it is the slot's progress.
 */
function slotDecoded(slot: Record<string, unknown>): number {
	const raw = slot.next_token;
	const entries = Array.isArray(raw) ? raw : raw && typeof raw === 'object' ? [raw] : [];
	let best = 0;
	for (const entry of entries) {
		const n = toInt((entry as Record<string, unknown>).n_decoded);
		if (n > best) best = n;
	}
	return best;
}

/**
 * Read a llama-server `/slots` document.
 *
 * The prompt counters come from the first slot that is processing (the first
 * slot when none is), because those two numbers describe one request's prefill
 * and a sum over idle slots would be meaningless.
 */
export function parseSlots(json: unknown): ServerReading {
	if (!Array.isArray(json)) return { ...UNREACHABLE };
	const slots = json.filter((s): s is Record<string, unknown> => !!s && typeof s === 'object');
	let processing = 0;
	let decoded = 0;
	const perSlot: number[] = [];
	for (const slot of slots) {
		if (slot.is_processing) processing += 1;
		const n = slotDecoded(slot);
		decoded += n;
		perSlot.push(n);
	}
	const front = slots.find((s) => s.is_processing) ?? slots[0];
	return {
		reachable: true,
		processing,
		slotCount: slots.length,
		promptTokens: front ? toInt(front.n_prompt_tokens) : 0,
		promptProcessed: front ? toInt(front.n_prompt_tokens_processed) : 0,
		decoded,
		slots: perSlot,
		cumulative: false
	};
}

/**
 * Read a Prometheus `/metrics` exposition. `decoded` is the server's lifetime
 * `tokens_predicted_total`; `Fleet` subtracts a baseline taken when the followed
 * run changed, which is how watch.sh turns the counter into a per-run number.
 */
export function parseMetrics(text: string): ServerReading {
	if (typeof text !== 'string' || text.trim() === '') return { ...UNREACHABLE };
	const values = new Map<string, number>();
	for (const line of text.split('\n')) {
		if (line.startsWith('#') || line.includes('{')) continue;
		const sep = line.indexOf(' ');
		if (sep <= 0) continue;
		const n = Number(line.slice(sep + 1).trim());
		if (Number.isFinite(n)) values.set(line.slice(0, sep), n);
	}
	if (values.size === 0) return { ...UNREACHABLE };
	return {
		reachable: true,
		processing: (values.get('llamacpp:requests_processing') ?? 0) > 0 ? 1 : 0,
		slotCount: 0,
		promptTokens: 0,
		promptProcessed: 0,
		decoded: Math.max(0, values.get('llamacpp:tokens_predicted_total') ?? 0),
		slots: [],
		cumulative: true
	};
}

export type FetchLike = typeof fetch;

async function getText(
	url: string,
	timeoutMs: number,
	fetchImpl: FetchLike
): Promise<{ status: number; body: string } | null> {
	const abort = new AbortController();
	const timer = setTimeout(() => abort.abort(), timeoutMs);
	try {
		const res = await fetchImpl(url, { signal: abort.signal, headers: { accept: '*/*' } });
		return { status: res.status, body: res.ok ? await res.text() : '' };
	} catch {
		return null;
	} finally {
		clearTimeout(timer);
	}
}

export interface SampleResult {
	reading: ServerReading;
	/** The mode actually used, so the caller can remember it. */
	mode: ProbeMode;
}

/**
 * Sample one server. `mode` is the mode already decided for it; `/slots` falls
 * back to `/metrics` when the endpoint is missing or unreadable, and the mode
 * that answered is returned so a caller can pin it for the rest of the session.
 */
export async function sampleServer(
	baseUrl: string,
	mode: ProbeMode,
	timeoutMs: number = PROBE_TIMEOUT_MS,
	fetchImpl: FetchLike = fetch
): Promise<SampleResult> {
	if (mode === 'slots') {
		const res = await getText(`${baseUrl}/slots`, timeoutMs, fetchImpl);
		// No answer at all is not evidence about the endpoint: a server busy with a
		// round misses the 400 ms budget, and a mode pinned on that would keep the
		// monitor on the lifetime counter for the rest of the session.
		if (res === null) return { reading: { ...UNREACHABLE }, mode: 'slots' };
		if (res.status >= 200 && res.status < 300) {
			try {
				const reading = parseSlots(JSON.parse(res.body));
				if (reading.reachable) return { reading, mode: 'slots' };
			} catch {
				// fall through to /metrics: a body that is not JSON is not a slots endpoint
			}
		}
		// 404 (or anything unusable) means this build serves only /metrics.
	}
	const res = await getText(`${baseUrl}/metrics`, timeoutMs, fetchImpl);
	if (res && res.status >= 200 && res.status < 300) {
		const reading = parseMetrics(res.body);
		if (reading.reachable) return { reading, mode: 'metrics' };
	}
	// Unreachable: keep the mode undecided by reporting the one we were asked for.
	return { reading: { ...UNREACHABLE, cumulative: mode === 'metrics' }, mode };
}

/** Probe `cmoa serve` for reachability only. It is never asked to do work. */
export async function sampleServe(
	listen: string,
	timeoutMs: number = PROBE_TIMEOUT_MS,
	fetchImpl: FetchLike = fetch
): Promise<boolean> {
	if (!listen) return false;
	const url = /^https?:\/\//.test(listen) ? listen : `http://${listen}`;
	const res = await getText(`${url}/v1/models`, timeoutMs, fetchImpl);
	return !!res && res.status >= 200 && res.status < 300;
}

interface ServerState {
	id: string;
	role: 'proposer' | 'judge';
	baseUrl: string;
	model: string;
	mode: ProbeMode;
	/** `/metrics` only: the counter value when the followed run last changed. */
	baseline: number | null;
	prevDecoded: number;
	prevAtMs: number;
	tokPerSec: number;
}

export interface FleetOptions {
	timeoutMs?: number;
	fetchImpl?: FetchLike;
	/** Rewrite loopback probe URLs through this host. Empty means no rewrite. */
	gateway?: string;
}

/**
 * Holds the per-server state a single `/slots` document cannot carry: which
 * endpoint answers, the `/metrics` baseline, and the smoothed decode rate.
 */
export class Fleet {
	private readonly servers: ServerState[];
	private readonly serveListen: string | null;
	private readonly timeoutMs: number;
	private readonly fetchImpl: FetchLike;
	private readonly gateway: string;
	private last: FleetState;

	constructor(config: MonitorConfig, options: FleetOptions = {}) {
		this.timeoutMs = options.timeoutMs ?? PROBE_TIMEOUT_MS;
		this.fetchImpl = options.fetchImpl ?? fetch;
		this.gateway = options.gateway ?? '';
		this.servers = config.proposers.map((p) => ({
			id: p.id,
			role: 'proposer' as const,
			baseUrl: p.baseUrl,
			model: p.model,
			mode: 'slots' as ProbeMode,
			baseline: null,
			prevDecoded: 0,
			prevAtMs: 0,
			tokPerSec: 0
		}));
		if (config.judge) {
			this.servers.push({
				id: JUDGE_ID,
				role: 'judge',
				baseUrl: config.judge.baseUrl,
				model: config.judge.model,
				mode: 'slots',
				baseline: null,
				prevDecoded: 0,
				prevAtMs: 0,
				tokPerSec: 0
			});
		}
		this.serveListen = config.serve?.listen || null;
		this.last = { sampledAtMs: 0, servers: [], serve: null };
	}

	/** The last sampled state, for callers that must not trigger a probe. */
	get state(): FleetState {
		return this.last;
	}

	/**
	 * Forget the `/metrics` baselines. watch.sh does this whenever the run it
	 * follows changes, so a lifetime counter starts measuring the new run.
	 */
	resetBaselines(): void {
		for (const server of this.servers) server.baseline = null;
	}

	async sample(nowMs: number = Date.now()): Promise<FleetState> {
		const [readings, serveReachable] = await Promise.all([
			Promise.all(
				this.servers.map((server) =>
					sampleServer(
						viaGateway(server.baseUrl, this.gateway),
						server.mode,
						this.timeoutMs,
						this.fetchImpl
					)
				)
			),
			this.serveListen
				? sampleServe(viaGateway(this.serveListen, this.gateway), this.timeoutMs, this.fetchImpl)
				: Promise.resolve(false)
		]);

		const samples: FleetSample[] = this.servers.map((server, i) => {
			const { reading, mode } = readings[i];
			if (reading.reachable) server.mode = mode;
			let decoded = reading.decoded;
			if (reading.cumulative) {
				if (server.baseline === null) server.baseline = decoded;
				decoded = Math.max(0, decoded - server.baseline);
			}
			this.smooth(server, decoded, nowMs);
			return {
				id: server.id,
				role: server.role,
				baseUrl: server.baseUrl,
				model: server.model,
				mode: server.mode,
				...reading,
				decoded,
				tokPerSec: server.tokPerSec
			};
		});

		this.last = {
			sampledAtMs: nowMs,
			servers: samples,
			serve: this.serveListen ? { listen: this.serveListen, reachable: serveReachable } : null
		};
		return this.last;
	}

	/**
	 * watch.sh's rate estimate: the tokens decoded since the previous tick over
	 * the elapsed time, averaged with the previous estimate. Halving the old
	 * value each tick is a cheap low-pass that keeps the number readable while
	 * a decode stalls, without a rolling window to carry around.
	 */
	private smooth(server: ServerState, decoded: number, nowMs: number): void {
		const dtMs = nowMs - server.prevAtMs;
		if (server.prevAtMs > 0 && dtMs > 0) {
			const delta = Math.max(0, decoded - server.prevDecoded);
			server.tokPerSec = (server.tokPerSec + (delta * 1000) / dtMs) / 2;
		}
		server.prevDecoded = decoded;
		server.prevAtMs = nowMs;
	}
}
