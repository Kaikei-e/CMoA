import { clock } from './format';

/**
 * The vocabulary of the COMM panel, and the pure parts of its store.
 *
 * Kept apart from `comm.svelte.ts` so the formatting and the transcript rule
 * can be tested as plain functions, with no runtime and no reactivity.
 */

/** What the `cmoa` extension of one completion says about the round behind it. */
export interface CommRun {
	id: string;
	/** `selected`, and in principle any other selection kind CMoA records. */
	selectionKind: string;
	reason: string;
	/** Judge calls made: two per pair. */
	calls: number;
	swapConsistent: number;
	latencyMs: number;
}

export interface CommMessage {
	role: 'user' | 'assistant';
	content: string;
	/** Epoch milliseconds, for the local ordering only. */
	at: number;
	/**
	 * A user turn the pool did not answer. It stays on screen so the person can
	 * edit and send it again, and it is never forwarded as history: a round that
	 * never happened is not part of the conversation.
	 */
	unanswered?: boolean;
	/** Assistant turns only. */
	run?: CommRun;
}

export interface CommFault {
	/** CMoA's error `type`, e.g. `no_candidate`, or `monitor` for a local fault. */
	type: string;
	/** CMoA's error `code`, e.g. `invalid_output`. Empty when there is none. */
	code: string;
	message: string;
	/** The run id, which CMoA puts in `param` on an error it produced. */
	runId?: string;
}

/** The wire shape of the `cmoa` extension `cmoa serve` adds to a completion. */
export interface CmoaExtension {
	run_id?: string;
	selection?: { kind?: string; reason?: string };
	judge?: {
		calls?: number;
		swap_consistent_pairs?: number;
		invalid_output_retries?: number;
		latency_ms?: number;
	};
	candidates?: { asked?: number; ok?: number };
}

function count(value: unknown): number {
	return typeof value === 'number' && Number.isFinite(value) ? value : 0;
}

/** The run metadata of one answer, or null when the pool sent no extension. */
export function runOfExtension(ext: CmoaExtension | undefined | null): CommRun | null {
	if (!ext || typeof ext !== 'object' || !ext.run_id) return null;
	return {
		id: ext.run_id,
		selectionKind: ext.selection?.kind ?? 'selected',
		reason: ext.selection?.reason ?? '',
		calls: count(ext.judge?.calls),
		swapConsistent: count(ext.judge?.swap_consistent_pairs),
		latencyMs: count(ext.judge?.latency_ms)
	};
}

/** The fault one error body describes, whoever wrote it. */
export function faultOfBody(body: unknown, status: number): CommFault {
	const error = (body as { error?: Record<string, unknown> } | null)?.error;
	if (!error || typeof error !== 'object') {
		return { type: 'monitor', code: '', message: `cmoa serve answered ${status}` };
	}
	const runId = typeof error.param === 'string' && error.param ? error.param : undefined;
	return {
		type: typeof error.type === 'string' && error.type ? error.type : 'error',
		code: typeof error.code === 'string' ? error.code : '',
		message: typeof error.message === 'string' ? error.message : `cmoa serve answered ${status}`,
		runId
	};
}

/**
 * The conversation to forward. Unanswered user turns are dropped: they are on
 * screen because the person may still want them, not because the pool ever saw
 * them, and sending one again as history would ask the fleet to answer a
 * question twice in the same round.
 */
export function forwardableTranscript(
	messages: CommMessage[]
): { role: 'user' | 'assistant'; content: string }[] {
	return messages
		.filter((message) => !(message.role === 'user' && message.unanswered))
		.map((message) => ({ role: message.role, content: message.content }));
}

/**
 * Everything of the metadata line after the run id — ` · selected · 2/3
 * swap-consistent · 23.2s` — because the run id itself is a button.
 */
export function metaRest(run: CommRun): string {
	const parts = [run.selectionKind];
	const pairs = Math.floor(run.calls / 2);
	if (pairs > 0) parts.push(`${run.swapConsistent}/${pairs} swap-consistent`);
	if (run.latencyMs > 0) parts.push(clock(run.latencyMs / 1000));
	return parts.map((part) => ` · ${part}`).join('');
}

/** `run 2026… · selected · 2/3 swap-consistent · 23.2s` */
export function metaLine(run: CommRun): string {
	return `run ${run.id}${metaRest(run)}`;
}

/** `NO CANDIDATE (invalid_output)` — the type as a word, the code as CMoA spells it. */
export function faultLabel(fault: CommFault): string {
	const head = fault.type.replace(/_/g, ' ').toUpperCase();
	return fault.code && fault.code !== fault.type ? `${head} (${fault.code})` : head;
}
