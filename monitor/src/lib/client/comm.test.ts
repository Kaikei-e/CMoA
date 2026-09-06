import { describe, expect, it } from 'vitest';
import {
	faultLabel,
	faultOfBody,
	forwardableTranscript,
	metaLine,
	runOfExtension,
	type CommMessage,
	type CommRun
} from './comm';

const RUN: CommRun = {
	id: '20260101T000000Z-11111111',
	selectionKind: 'selected',
	reason: 'majority',
	calls: 6,
	swapConsistent: 2,
	latencyMs: 23_200
};

function user(content: string, unanswered = false): CommMessage {
	return { role: 'user', content, at: 0, unanswered: unanswered || undefined };
}

describe('forwardableTranscript', () => {
	it('keeps the answered turns in order and drops the unanswered ones', () => {
		const messages: CommMessage[] = [
			user('one'),
			{ role: 'assistant', content: 'two', at: 1, run: RUN },
			user('never asked', true),
			user('three')
		];
		expect(forwardableTranscript(messages)).toEqual([
			{ role: 'user', content: 'one' },
			{ role: 'assistant', content: 'two' },
			{ role: 'user', content: 'three' }
		]);
	});

	it('carries nothing but the role and the content', () => {
		expect(forwardableTranscript([user('one')])).toEqual([{ role: 'user', content: 'one' }]);
	});
});

describe('metaLine', () => {
	it('names the run, the selection, the swap consistency and the judge latency', () => {
		expect(metaLine(RUN)).toBe(
			'run 20260101T000000Z-11111111 · selected · 2/3 swap-consistent · 23.2s'
		);
	});

	it('leaves out what the pool did not report', () => {
		expect(metaLine({ ...RUN, calls: 0, latencyMs: 0 })).toBe(
			'run 20260101T000000Z-11111111 · selected'
		);
	});

	it('says minutes as a clock once a round runs past one', () => {
		expect(metaLine({ ...RUN, latencyMs: 115_500 })).toContain('1:55.5');
	});
});

describe('runOfExtension', () => {
	it('reads the cmoa extension of a completion', () => {
		expect(
			runOfExtension({
				run_id: '20260101T000000Z-11111111',
				selection: { kind: 'selected', reason: 'majority' },
				judge: { calls: 6, swap_consistent_pairs: 2, invalid_output_retries: 0, latency_ms: 23_200 },
				candidates: { asked: 3, ok: 3 }
			})
		).toEqual(RUN);
	});

	it('is null when there is no extension to read', () => {
		expect(runOfExtension(null)).toBeNull();
		expect(runOfExtension({})).toBeNull();
	});
});

describe('faultOfBody and faultLabel', () => {
	it('reads a no_candidate error, run id included', () => {
		const fault = faultOfBody(
			{
				error: {
					message: 'no candidate was selected: invalid_output',
					type: 'no_candidate',
					code: 'invalid_output',
					param: '20260101T001000Z-22222222'
				}
			},
			502
		);
		expect(fault.runId).toBe('20260101T001000Z-22222222');
		expect(faultLabel(fault)).toBe('NO CANDIDATE (invalid_output)');
	});

	it('falls back to the status when the body says nothing', () => {
		const fault = faultOfBody(null, 500);
		expect(fault).toEqual({ type: 'monitor', code: '', message: 'cmoa serve answered 500' });
		expect(faultLabel(fault)).toBe('MONITOR');
	});

	it('does not repeat a code that only restates the type', () => {
		expect(faultLabel({ type: 'judge_timeout', code: 'judge_timeout', message: '' })).toBe(
			'JUDGE TIMEOUT'
		);
	});
});
