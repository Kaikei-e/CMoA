import { describe, expect, it } from 'vitest';
import { MAX_BODY_BYTES, relayChat, validateMessages } from './chat';
import { POST } from '../../routes/api/chat/+server';
import { stubFetch } from './testing';
import type { ServeConfig } from '$lib/types';

const SERVE: ServeConfig = {
	listen: '127.0.0.1:8495',
	runsDir: '/srv/example/serve-runs',
	poolName: 'cmoa'
};

const UPSTREAM = 'http://127.0.0.1:8495/v1/chat/completions';

const COMPLETION = JSON.stringify({
	id: 'chatcmpl-20260101T000000Z-11111111',
	choices: [{ index: 0, message: { role: 'assistant', content: 'an answer' }, finish_reason: 'stop' }],
	cmoa: {
		run_id: '20260101T000000Z-11111111',
		selection: { kind: 'selected', reason: 'majority' },
		judge: { calls: 6, swap_consistent_pairs: 2, invalid_output_retries: 0, latency_ms: 23_200 },
		candidates: { asked: 3, ok: 3 }
	}
});

const NO_CANDIDATE = JSON.stringify({
	error: {
		message: 'no candidate was selected: invalid_output',
		type: 'no_candidate',
		code: 'invalid_output',
		param: '20260101T001000Z-22222222'
	}
});

function ask(body: unknown): Request {
	return new Request('http://monitor/api/chat', {
		method: 'POST',
		headers: { 'content-type': 'application/json' },
		body: typeof body === 'string' ? body : JSON.stringify(body)
	});
}

const HELLO = { messages: [{ role: 'user', content: 'hello' }] };

describe('validateMessages', () => {
	it('accepts a conversation that ends on a user turn', () => {
		const checked = validateMessages([
			{ role: 'system', content: 'be brief' },
			{ role: 'user', content: 'one' },
			{ role: 'assistant', content: 'two' },
			{ role: 'user', content: 'three' }
		]);
		expect(checked).toEqual({
			ok: true,
			messages: [
				{ role: 'system', content: 'be brief' },
				{ role: 'user', content: 'one' },
				{ role: 'assistant', content: 'two' },
				{ role: 'user', content: 'three' }
			]
		});
	});

	it('refuses anything that is not a conversation to answer', () => {
		expect(validateMessages(null)).toMatchObject({ ok: false });
		expect(validateMessages([])).toMatchObject({ ok: false });
		expect(validateMessages([{ role: 'user', content: '   ' }])).toMatchObject({ ok: false });
		expect(validateMessages([{ role: 'user', content: 42 }])).toMatchObject({ ok: false });
		expect(validateMessages([{ role: 'tool', content: 'x' }])).toMatchObject({ ok: false });
		// a transcript whose last turn is the assistant's has nothing to answer
		expect(
			validateMessages([
				{ role: 'user', content: 'one' },
				{ role: 'assistant', content: 'two' }
			])
		).toMatchObject({ ok: false });
	});
});

describe('relayChat', () => {
	it('forwards the pool name, the messages and stream: false', async () => {
		const { fetch, requests } = stubFetch({ [UPSTREAM]: { status: 200, body: COMPLETION } });
		const response = await relayChat(
			ask({
				messages: [
					{ role: 'user', content: 'one' },
					{ role: 'assistant', content: 'two' },
					{ role: 'user', content: 'three' }
				]
			}),
			{ serve: { ...SERVE, poolName: 'pool-a' }, fetch }
		);

		expect(response.status).toBe(200);
		expect(requests).toHaveLength(1);
		expect(requests[0].url).toBe(UPSTREAM);
		expect(requests[0].init?.method).toBe('POST');
		expect(JSON.parse(String(requests[0].init?.body))).toEqual({
			model: 'pool-a',
			stream: false,
			messages: [
				{ role: 'user', content: 'one' },
				{ role: 'assistant', content: 'two' },
				{ role: 'user', content: 'three' }
			]
		});
	});

	it('passes a 200 through with its cmoa extension intact', async () => {
		const { fetch } = stubFetch({ [UPSTREAM]: { status: 200, body: COMPLETION } });
		const response = await relayChat(ask(HELLO), { serve: SERVE, fetch });
		expect(response.headers.get('cache-control')).toContain('no-store');
		const body = await response.json();
		expect(body.choices[0].message.content).toBe('an answer');
		expect(body.cmoa.selection.kind).toBe('selected');
		expect(body.cmoa.judge.swap_consistent_pairs).toBe(2);
	});

	it('passes a 502 no_candidate through with its run id', async () => {
		const { fetch } = stubFetch({ [UPSTREAM]: { status: 502, body: NO_CANDIDATE } });
		const response = await relayChat(ask(HELLO), { serve: SERVE, fetch });
		expect(response.status).toBe(502);
		const body = await response.json();
		expect(body.error.type).toBe('no_candidate');
		expect(body.error.code).toBe('invalid_output');
		expect(body.error.param).toBe('20260101T001000Z-22222222');
	});

	it('maps a network failure to 502 naming the listen address', async () => {
		const { fetch } = stubFetch({
			[UPSTREAM]: () => {
				throw new TypeError('fetch failed');
			}
		});
		const response = await relayChat(ask(HELLO), { serve: SERVE, fetch });
		expect(response.status).toBe(502);
		const body = await response.json();
		expect(body.error).toEqual({
			message: 'cmoa serve unreachable at 127.0.0.1:8495',
			type: 'monitor'
		});
	});

	it('rejects a bad body with 400 and asks the pool for nothing', async () => {
		const { fetch, calls } = stubFetch({ [UPSTREAM]: { status: 200, body: COMPLETION } });
		for (const body of [
			'not json',
			JSON.stringify([]),
			JSON.stringify({ messages: [] }),
			JSON.stringify({ messages: [{ role: 'assistant', content: 'last word' }] })
		]) {
			const response = await relayChat(ask(body), { serve: SERVE, fetch });
			expect(response.status).toBe(400);
			expect((await response.json()).error.type).toBe('invalid_request_error');
		}
		expect(calls).toEqual([]);
	});

	it('refuses a body over the cap before reading it', async () => {
		const { fetch, calls } = stubFetch({ [UPSTREAM]: { status: 200, body: COMPLETION } });
		const request = new Request('http://monitor/api/chat', {
			method: 'POST',
			headers: { 'content-type': 'application/json', 'content-length': String(MAX_BODY_BYTES + 1) },
			body: JSON.stringify(HELLO)
		});
		const response = await relayChat(request, { serve: SERVE, fetch });
		expect(response.status).toBe(413);
		expect(calls).toEqual([]);
	});

	it('says so when the config declares no serve block', async () => {
		const { fetch, calls } = stubFetch({});
		const response = await relayChat(ask(HELLO), { serve: null, fetch });
		expect(response.status).toBe(503);
		expect(await response.json()).toEqual({
			error: { message: 'cmoa.json declares no serve block', type: 'monitor' }
		});
		expect(calls).toEqual([]);
	});
});

describe('POST /api/chat', () => {
	it('answers 503 with the reason when there is no config at all', async () => {
		// The suite runs without CMOA_CONFIG, which is the same fault a
		// misconfigured deployment shows on every other route.
		const response = await POST({ request: ask(HELLO) } as never);
		expect(response.status).toBe(503);
		const body = await response.json();
		expect(body.error.type).toBe('monitor');
		expect(body.error.message).toMatch(/CMOA_CONFIG/);
	});
});
