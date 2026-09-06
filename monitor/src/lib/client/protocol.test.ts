import { describe, expect, it } from 'vitest';
import { BACKOFF_CAP_MS, nextDelay, parseEvent } from './protocol';

describe('nextDelay', () => {
	const mid = () => 0.5; // the jitter factor is exactly 1 at the midpoint

	it('doubles from one second and stops at the cap', () => {
		expect([0, 1, 2, 3, 4, 5, 9].map((n) => nextDelay(n, mid))).toEqual([
			1000, 2000, 4000, 8000, 15_000, 15_000, 15_000
		]);
	});

	it('never waits longer than a quarter either side of the schedule', () => {
		for (const attempt of [0, 1, 2, 3, 4, 10]) {
			expect(nextDelay(attempt, () => 0)).toBe(
				Math.round(Math.min(BACKOFF_CAP_MS, 1000 * 2 ** attempt) * 0.75)
			);
			expect(nextDelay(attempt, () => 1)).toBe(
				Math.round(Math.min(BACKOFF_CAP_MS, 1000 * 2 ** attempt) * 1.25)
			);
		}
	});

	it('treats a nonsense attempt count as the first attempt', () => {
		expect(nextDelay(-3, mid)).toBe(1000);
		expect(nextDelay(0.9, mid)).toBe(1000);
	});
});

describe('parseEvent', () => {
	it('reads each of the four state events', () => {
		expect(parseEvent('fleet', '{"sampledAtMs":1,"servers":[],"serve":null}')).toEqual({
			kind: 'fleet',
			fleet: { sampledAtMs: 1, servers: [], serve: null }
		});
		expect(parseEvent('run', '{"header":{"runId":"r"}}')).toMatchObject({ kind: 'run' });
		expect(parseEvent('runs', '[{"id":"r"}]')).toMatchObject({ kind: 'runs' });
		expect(parseEvent('calibration', '{"dir":null,"entries":[]}')).toMatchObject({
			kind: 'calibration'
		});
	});

	it('carries the message of an error event', () => {
		expect(parseEvent('error', '{"message":"CMOA_CONFIG is not set"}')).toEqual({
			kind: 'error',
			message: 'CMOA_CONFIG is not set'
		});
		expect(parseEvent('error', '{}')).toEqual({
			kind: 'error',
			message: 'the monitor reported an error with no message'
		});
	});

	it('drops a frame that is not ours, is not JSON, or is the wrong shape', () => {
		expect(parseEvent('keep-alive', '{}')).toBeNull();
		expect(parseEvent('fleet', 'not json')).toBeNull();
		expect(parseEvent('fleet', '{"servers":"no"}')).toBeNull();
		expect(parseEvent('run', '[]')).toBeNull();
		expect(parseEvent('runs', '{"runs":[]}')).toBeNull();
		expect(parseEvent('calibration', '{"dir":null}')).toBeNull();
	});
});
