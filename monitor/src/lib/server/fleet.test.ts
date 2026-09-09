import { describe, expect, it } from 'vitest';
import { Fleet, parseMetrics, parseSlots, sampleServe, sampleServer } from './fleet';
import { loadConfig } from './config';
import { fixture, fixtureJson, fixtureText, stubFetch } from './testing';

describe('parseSlots', () => {
	it('reads an idle single-slot server', () => {
		const reading = parseSlots(fixtureJson('fleet', 'slots-idle.json'));
		expect(reading.reachable).toBe(true);
		expect(reading.processing).toBe(0);
		expect(reading.decoded).toBe(0);
		expect(reading.promptTokens).toBe(412);
		expect(reading.slots).toEqual([0]);
		expect(reading.cumulative).toBe(false);
	});

	it('reads a generating server', () => {
		const reading = parseSlots(fixtureJson('fleet', 'slots-generating.json'));
		expect(reading.processing).toBe(1);
		expect(reading.decoded).toBe(137);
		expect(reading.promptProcessed).toBe(412);
	});

	it('reads a prefilling server as processing with nothing decoded', () => {
		const reading = parseSlots(fixtureJson('fleet', 'slots-prefill.json'));
		expect(reading.processing).toBe(1);
		expect(reading.decoded).toBe(0);
		expect(reading.promptProcessed).toBe(480);
		expect(reading.promptTokens).toBe(1556);
	});

	it('accepts next_token as a bare object as well as a list', () => {
		expect(parseSlots(fixtureJson('fleet', 'slots-next-token-object.json')).decoded).toBe(42);
	});

	it('sums a multi-slot judge and keeps the per-slot counts', () => {
		const reading = parseSlots(fixtureJson('fleet', 'slots-judge.json'));
		expect(reading.slotCount).toBe(3);
		expect(reading.processing).toBe(2);
		expect(reading.decoded).toBe(42);
		expect(reading.slots).toEqual([30, 12, 0]);
		// the prompt counters describe one request, so they come from a busy slot
		expect(reading.promptTokens).toBe(1556);
	});

	it('treats anything that is not a slots array as unreachable', () => {
		expect(parseSlots(null).reachable).toBe(false);
		expect(parseSlots({ error: 'not found' }).reachable).toBe(false);
		expect(parseSlots([]).reachable).toBe(true);
	});
});

describe('parseMetrics', () => {
	it('reads the two gauges the display needs and ignores labelled series', () => {
		const reading = parseMetrics(fixtureText('fleet', 'metrics.txt'));
		expect(reading.reachable).toBe(true);
		expect(reading.processing).toBe(1);
		expect(reading.decoded).toBe(2102);
		expect(reading.cumulative).toBe(true);
		expect(reading.slots).toEqual([]);
	});

	it('treats an empty or commentary-only body as unreachable', () => {
		expect(parseMetrics('').reachable).toBe(false);
		expect(parseMetrics('# HELP only\n').reachable).toBe(false);
	});
});

describe('sampleServer', () => {
	it('uses /slots when it answers', async () => {
		const { fetch, calls } = stubFetch({
			'http://server/slots': {
				status: 200,
				body: JSON.stringify(fixtureJson('fleet', 'slots-generating.json'))
			}
		});
		const result = await sampleServer('http://server', 'slots', 50, fetch);
		expect(result.mode).toBe('slots');
		expect(result.reading.decoded).toBe(137);
		expect(calls).toEqual(['http://server/slots']);
	});

	it('falls back to /metrics when /slots is a 404', async () => {
		const { fetch, calls } = stubFetch({
			'http://server/metrics': { status: 200, body: fixtureText('fleet', 'metrics.txt') }
		});
		const result = await sampleServer('http://server', 'slots', 50, fetch);
		expect(result.mode).toBe('metrics');
		expect(result.reading.decoded).toBe(2102);
		expect(calls).toEqual(['http://server/slots', 'http://server/metrics']);
	});

	it('goes straight to /metrics once that mode is decided', async () => {
		const { fetch, calls } = stubFetch({
			'http://server/metrics': { status: 200, body: fixtureText('fleet', 'metrics.txt') }
		});
		await sampleServer('http://server', 'metrics', 50, fetch);
		expect(calls).toEqual(['http://server/metrics']);
	});

	it('reports a server that answers neither endpoint as unreachable', async () => {
		const { fetch } = stubFetch({});
		const result = await sampleServer('http://server', 'slots', 50, fetch);
		expect(result.reading.reachable).toBe(false);
	});

	it('keeps the slots mode when /slots merely times out, and asks nothing else', async () => {
		const calls: string[] = [];
		const slow = (async (input: RequestInfo | URL, init?: RequestInit) => {
			calls.push(String(input));
			await new Promise<void>((_, reject) =>
				init?.signal?.addEventListener('abort', () => reject(new Error('aborted')))
			);
			throw new Error('unreachable');
		}) as typeof fetch;
		const result = await sampleServer('http://server', 'slots', 20, slow);
		expect(result.mode).toBe('slots');
		expect(result.reading.reachable).toBe(false);
		expect(calls).toEqual(['http://server/slots']);
	});
});

describe('sampleServe', () => {
	it('asks cmoa serve only for the model list', async () => {
		const { fetch, calls } = stubFetch({
			'http://127.0.0.1:8095/v1/models': { status: 200, body: '{"data":[]}' }
		});
		expect(await sampleServe('127.0.0.1:8095', 50, fetch)).toBe(true);
		expect(calls).toEqual(['http://127.0.0.1:8095/v1/models']);
	});

	it('is false when serve is down', async () => {
		const { fetch } = stubFetch({});
		expect(await sampleServe('127.0.0.1:8095', 50, fetch)).toBe(false);
	});
});

describe('Fleet', () => {
	const config = loadConfig(fixture('config', 'cmoa.json'));

	function slotsBody(decoded: number): string {
		return JSON.stringify([
			{ is_processing: true, n_prompt_tokens: 412, n_prompt_tokens_processed: 412, next_token: [{ n_decoded: decoded }] }
		]);
	}

	it('smooths the decode rate the way watch.sh does', async () => {
		let decoded = 0;
		const impl = (async (input: RequestInfo | URL) => {
			const url = String(input);
			if (url.endsWith('/slots')) return new Response(slotsBody(decoded), { status: 200 });
			return new Response('', { status: 404 });
		}) as typeof fetch;

		const fleet = new Fleet(config, { timeoutMs: 50, fetchImpl: impl });
		await fleet.sample(1000); // first tick only records the baseline
		expect(fleet.state.servers[0].tokPerSec).toBe(0);

		decoded = 10;
		await fleet.sample(2000); // (0 + 10 * 1000/1000) / 2
		expect(fleet.state.servers[0].tokPerSec).toBe(5);

		decoded = 30;
		await fleet.sample(3000); // (5 + 20) / 2
		expect(fleet.state.servers[0].tokPerSec).toBe(12.5);

		// a counter that went backwards (a fresh request) never yields a negative rate
		decoded = 0;
		await fleet.sample(4000);
		expect(fleet.state.servers[0].tokPerSec).toBe(6.25);
	});

	it('samples the judge alongside the proposers and probes serve', async () => {
		const impl = (async (input: RequestInfo | URL) => {
			const url = String(input);
			if (url.endsWith('/slots')) return new Response(slotsBody(3), { status: 200 });
			if (url.endsWith('/v1/models')) return new Response('{"data":[]}', { status: 200 });
			return new Response('', { status: 404 });
		}) as typeof fetch;
		const fleet = new Fleet(config, { timeoutMs: 50, fetchImpl: impl });
		const state = await fleet.sample(1000);
		expect(state.servers.map((s) => s.id)).toEqual(['granite', 'qwen', 'gemma', '__judge__']);
		expect(state.servers[3].role).toBe('judge');
		expect(state.serve).toEqual({ listen: '127.0.0.1:8095', reachable: true });
	});

	it('probes loopback servers through CMOA_MONITOR_GATEWAY', async () => {
		const seen: string[] = [];
		const impl = (async (input: RequestInfo | URL) => {
			seen.push(String(input));
			const url = String(input);
			if (url.endsWith('/slots')) return new Response(slotsBody(1), { status: 200 });
			if (url.endsWith('/v1/models')) return new Response('{"data":[]}', { status: 200 });
			return new Response('', { status: 404 });
		}) as typeof fetch;
		const fleet = new Fleet(config, { timeoutMs: 50, fetchImpl: impl, gateway: 'host.docker.internal' });
		const state = await fleet.sample(1000);
		expect(seen.some((u) => u.startsWith('http://host.docker.internal:8081/'))).toBe(true);
		expect(seen.some((u) => u === 'http://host.docker.internal:8095/v1/models')).toBe(true);
		expect(state.servers[0].baseUrl).toBe('http://127.0.0.1:8081');
		expect(state.serve).toEqual({ listen: '127.0.0.1:8095', reachable: true });
	});

	it('subtracts a /metrics baseline and re-zeroes it on demand', async () => {
		let total = 2000;
		const impl = (async (input: RequestInfo | URL) => {
			const url = String(input);
			if (url.endsWith('/metrics'))
				return new Response(
					`llamacpp:requests_processing 1\nllamacpp:tokens_predicted_total ${total}\n`,
					{ status: 200 }
				);
			return new Response('', { status: 404 });
		}) as typeof fetch;

		const fleet = new Fleet(config, { timeoutMs: 50, fetchImpl: impl });
		await fleet.sample(1000);
		expect(fleet.state.servers[0].decoded).toBe(0);
		expect(fleet.state.servers[0].mode).toBe('metrics');

		total = 2150;
		await fleet.sample(2000);
		expect(fleet.state.servers[0].decoded).toBe(150);

		fleet.resetBaselines();
		await fleet.sample(3000);
		expect(fleet.state.servers[0].decoded).toBe(0);
	});
});
