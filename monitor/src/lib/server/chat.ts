import type { ServeConfig } from '$lib/types';
import { NO_STORE } from './http';

/**
 * The relay behind `POST /api/chat`.
 *
 * The monitor never runs a round. It forwards one OpenAI-shaped chat
 * completion to `cmoa serve`, which is what writes the task directory and the
 * run trace the rest of the screen is already watching. The answer — success or
 * error — is passed back as the pool wrote it, so the browser reads CMoA's own
 * vocabulary (`no_candidate`, `judge_timeout`) rather than a paraphrase.
 */

/** The roles a relayed conversation may carry, matching `task.ConvMessage`. */
export type ChatRole = 'user' | 'assistant' | 'system';

export interface ChatMessage {
	role: ChatRole;
	content: string;
}

/**
 * The cap on a relayed body. `cmoa serve` has its own `max_body_bytes`, and a
 * request over this one is refused here so a mistyped client cannot make the
 * monitor hold a megabyte per socket on its way to being refused anyway.
 */
export const MAX_BODY_BYTES = 1024 * 1024;

/**
 * How long one round may take. A chat round asks every proposer and then runs
 * the judge over every pair, twice; on a local fleet that is minutes, not
 * seconds, and a shorter timeout would only ever cancel work that was going to
 * succeed.
 */
export const UPSTREAM_TIMEOUT_MS = 10 * 60 * 1000;

const ROLES = new Set<string>(['user', 'assistant', 'system']);

export interface ChatRelayOptions {
	/** The `serve` block of the loaded cmoa.json, or null when there is none. */
	serve: ServeConfig | null;
	/** The reason there is no config at all, when reading it failed. */
	configError?: string | null;
	/** Injected in tests; the platform `fetch` otherwise. */
	fetch?: typeof globalThis.fetch;
}

/** The OpenAI error envelope, which is also CMoA's. */
function errorResponse(
	status: number,
	error: { message: string; type: string; param?: string; code?: string }
): Response {
	return new Response(JSON.stringify({ error }), {
		status,
		headers: { ...NO_STORE, 'content-type': 'application/json' }
	});
}

/**
 * Check a relayed conversation. The rules are the ones `cmoa serve` would
 * apply anyway; applying them here means an obvious mistake never reaches a
 * server whose every request writes a directory.
 */
export function validateMessages(value: unknown): { ok: true; messages: ChatMessage[] } | { ok: false; reason: string } {
	if (!Array.isArray(value)) return { ok: false, reason: 'messages must be an array' };
	if (value.length === 0) return { ok: false, reason: 'messages must not be empty' };
	const messages: ChatMessage[] = [];
	for (const [index, entry] of value.entries()) {
		if (!entry || typeof entry !== 'object' || Array.isArray(entry)) {
			return { ok: false, reason: `messages[${index}] is not an object` };
		}
		const { role, content } = entry as { role?: unknown; content?: unknown };
		if (typeof role !== 'string' || !ROLES.has(role)) {
			return { ok: false, reason: `messages[${index}].role must be user, assistant or system` };
		}
		if (typeof content !== 'string' || content.trim() === '') {
			return { ok: false, reason: `messages[${index}].content must be a non-empty string` };
		}
		messages.push({ role: role as ChatRole, content });
	}
	if (messages[messages.length - 1].role !== 'user') {
		return { ok: false, reason: 'the last message must be the user turn to answer' };
	}
	return { ok: true, messages };
}

/**
 * Relay one conversation to `cmoa serve` and hand back what it said.
 *
 * Every upstream answer is passed through untouched, status included: 200 with
 * `choices[0].message.content` and the `cmoa` extension, 502 for
 * `no_candidate` and `judge_failed`, 504 for `judge_timeout`, 400 for a
 * conversation the task refuses. A pool already busy with `max_inflight`
 * rounds does not answer at all until it is free — it queues rather than
 * refusing — which is why the timeout above is a round's length and not a
 * request's.
 */
export async function relayChat(request: Request, options: ChatRelayOptions): Promise<Response> {
	if (options.configError) {
		return errorResponse(503, { message: options.configError, type: 'monitor' });
	}
	const serve = options.serve;
	if (!serve || !serve.listen) {
		return errorResponse(503, {
			message: 'cmoa.json declares no serve block',
			type: 'monitor'
		});
	}

	const declared = Number(request.headers.get('content-length') ?? '');
	if (Number.isFinite(declared) && declared > MAX_BODY_BYTES) {
		return errorResponse(413, {
			message: `the body is over ${MAX_BODY_BYTES} bytes`,
			type: 'invalid_request_error'
		});
	}

	let text: string;
	try {
		text = await request.text();
	} catch (err) {
		return errorResponse(400, { message: (err as Error).message, type: 'invalid_request_error' });
	}
	if (new TextEncoder().encode(text).length > MAX_BODY_BYTES) {
		return errorResponse(413, {
			message: `the body is over ${MAX_BODY_BYTES} bytes`,
			type: 'invalid_request_error'
		});
	}

	let body: unknown;
	try {
		body = JSON.parse(text);
	} catch (err) {
		return errorResponse(400, {
			message: `the body is not a chat completion request: ${(err as Error).message}`,
			type: 'invalid_request_error'
		});
	}
	if (!body || typeof body !== 'object' || Array.isArray(body)) {
		return errorResponse(400, {
			message: 'the body is not a chat completion request',
			type: 'invalid_request_error'
		});
	}

	const checked = validateMessages((body as { messages?: unknown }).messages);
	if (!checked.ok) {
		return errorResponse(400, {
			message: checked.reason,
			type: 'invalid_request_error',
			param: 'messages'
		});
	}

	const fetchImpl = options.fetch ?? globalThis.fetch;
	const url = `http://${serve.listen}/v1/chat/completions`;
	let upstream: Response;
	try {
		upstream = await fetchImpl(url, {
			method: 'POST',
			headers: { 'content-type': 'application/json', accept: 'application/json' },
			// `stream: false` is not a preference: the monitor shows the round as
			// the trace on disk, and wants the whole completion in one piece.
			body: JSON.stringify({ model: serve.poolName || 'cmoa', messages: checked.messages, stream: false }),
			signal: AbortSignal.timeout(UPSTREAM_TIMEOUT_MS)
		});
	} catch (err) {
		const name = (err as Error).name;
		if (name === 'TimeoutError') {
			return errorResponse(504, {
				message: `cmoa serve did not answer within ${UPSTREAM_TIMEOUT_MS / 60000} minutes`,
				type: 'monitor'
			});
		}
		return errorResponse(502, {
			message: `cmoa serve unreachable at ${serve.listen}`,
			type: 'monitor'
		});
	}

	const answer = await upstream.text();
	return new Response(answer, {
		status: upstream.status,
		headers: { ...NO_STORE, 'content-type': 'application/json' }
	});
}
