import { beforeEach, describe, expect, it } from 'vitest';
import type { MonitorConfig, RunSnapshot } from '$lib/types';
import { loadConfig } from './config';
import { clearRunCache, discoverRuns, readRunFiles } from './runs';
import { colourOf, cut, deriveSnapshot, timestampMs, widthCut } from './snapshot';
import type { RunFiles } from './trace';
import { fixture, judgeSample, proposerSample } from './testing';

const config: MonitorConfig = loadConfig(fixture('config', 'cmoa.json'));
const NOW = Date.parse('2026-01-01T00:05:00Z');

beforeEach(() => {
	clearRunCache();
});

async function filesOf(...parts: string[]): Promise<RunFiles> {
	const [ref] = await discoverRuns([fixture(...parts)]);
	expect(ref).toBeDefined();
	return readRunFiles(ref);
}

async function snapshotOf(...parts: string[]): Promise<RunSnapshot> {
	return deriveSnapshot(await filesOf(...parts), [], config, NOW);
}

describe('colourOf', () => {
	it('sorts every state word into watch.sh four categories', () => {
		for (const word of ['ok', 'pass', 'done', 'selected']) expect(colourOf(word)).toBe('ok');
		for (const word of ['run', 'running', 'generating', 'prefill', 'busy'])
			expect(colourOf(word)).toBe('run');
		for (const word of ['bad', 'failed', 'invalid', 'timeout', 'error', 'unreachable', 'no_candidate'])
			expect(colourOf(word)).toBe('bad');
		for (const word of ['idle', 'pend', '-', 'skipped']) expect(colourOf(word)).toBe('dim');
	});
});

describe('text helpers', () => {
	it('collapses whitespace before cutting', () => {
		expect(cut('  a \n b   c ', 5)).toBe('a b c');
		expect(cut(undefined, 5)).toBe('');
	});

	it('counts a non-ASCII character as two columns', () => {
		expect(widthCut('abcdef', 3)).toBe('abc');
		expect(widthCut('あいう', 4)).toBe('あい');
	});

	it('reads a nanosecond timestamp', () => {
		expect(timestampMs('2026-01-01T00:00:13.300000000Z')).toBe(Date.parse('2026-01-01T00:00:13.300Z'));
		expect(timestampMs('')).toBeNull();
		expect(timestampMs('not a date')).toBeNull();
	});
});

describe('deriveSnapshot: chat run that selected a candidate', () => {
	it('reports the header and the phase', async () => {
		const snapshot = await snapshotOf('serve-root', 'chat-selected');
		expect(snapshot.header.runId).toBe('20260101T000000Z-11111111');
		expect(snapshot.header.face).toBe('chat');
		expect(snapshot.header.phase).toBe('done');
		expect(snapshot.header.complete).toBe(true);
		expect(snapshot.header.mode).toBe('follow');
		// a finished run measures to its own select.json, not to the wall clock
		expect(snapshot.header.elapsedSec).toBe(115.5);
	});

	it('shows every lane done, with the answer excerpt', async () => {
		const snapshot = await snapshotOf('serve-root', 'chat-selected');
		expect(snapshot.lanes.map((l) => l.id)).toEqual(['granite', 'qwen', 'gemma']);
		expect(snapshot.lanes.map((l) => l.state)).toEqual(['done', 'done', 'done']);
		expect(snapshot.lanes.map((l) => l.colour)).toEqual(['ok', 'ok', 'ok']);
		const gemma = snapshot.lanes[2];
		expect(gemma.candidate?.status).toBe('ok');
		expect(gemma.candidate?.info.startsWith('Candidate answer placeholder for gemma')).toBe(true);
		expect(gemma.candidate?.timing).toBe('13.3s 8.1tok/s');
		expect(gemma.candidate?.completionTokens).toBe(100);
		expect(snapshot.lanes[1].candidate?.reasoningBytes).toBe(412);
		// decoded comes from the candidate once it exists; 180 of a 512 budget
		expect(snapshot.lanes[0].decoded).toBe(180);
		expect(snapshot.lanes[0].barFraction).toBeCloseTo(180 / 512, 6);
		// the chat face has no verifier
		expect(gemma.verify).toBeNull();
	});

	it('fills the judge grid from judge.json', async () => {
		const snapshot = await snapshotOf('serve-root', 'chat-selected');
		const judge = snapshot.judge!;
		expect(judge.pairs.map((p) => p.label)).toEqual([
			'granite|qwen',
			'granite|gemma',
			'qwen|gemma'
		]);
		expect(judge.pairs[0].ab.text).toBe('ab ok A>granite 19.1s');
		expect(judge.pairs[0].ba.text).toBe('ba ok tie 13.1s');
		expect(judge.pairs[0].ba.who).toBe('tie');
		expect(judge.pairs[0].verdict).toMatchObject({
			state: 'bad',
			text: 'draw (tie)',
			provisional: false
		});
		expect(judge.pairs[1].verdict.text).toBe('-> gemma');
		expect(judge.pairs[1].verdict.colour).toBe('ok');
		expect(judge.wins).toEqual([
			{ id: 'granite', wins: 0 },
			{ id: 'qwen', wins: 0 },
			{ id: 'gemma', wins: 2 }
		]);
		expect(judge.swapConsistent).toEqual({ pairs: 2, total: 3 });
		expect(judge.retries).toBe(0);
		expect(judge.seedText).toBe('seed 7 (flag)  nonce 7f3a91c4');
		expect(judge.presentation).toEqual({ seed: '7', source: 'flag', nonce: '7f3a91c4' });
		expect(judge.outcome.state).toBe('selected');
		expect(judge.outcome.text).toBe(
			'selected gemma  condorcet winner, 2 of 3 pairs agreed under bo'
		);
		expect(judge.latencyText).toBe('100.0s');
	});

	it('reads the judge server out of the fleet sample', async () => {
		const files = await filesOf('serve-root', 'chat-selected');
		const snapshot = deriveSnapshot(
			files,
			[judgeSample({ processing: 2, slotCount: 6, decoded: 42, slots: [30, 12], tokPerSec: 9.5 })],
			config,
			NOW
		);
		expect(snapshot.judge?.server).toEqual({
			reachable: true,
			slotCount: 6,
			busy: 2,
			decoded: 42,
			tokPerSec: 9.5,
			slots: [30, 12]
		});
	});

	it('reports the selection and the ranking', async () => {
		const snapshot = await snapshotOf('serve-root', 'chat-selected');
		expect(snapshot.select).toMatchObject({
			state: 'selected',
			colour: 'ok',
			text: 'selected gemma',
			kind: 'selected'
		});
		expect(snapshot.select.ranked).toEqual(['gemma', 'granite', 'qwen']);
		expect(snapshot.select.alsoPassed).toEqual([]);
	});

	it('lays the phases out in order from created_at', async () => {
		const snapshot = await snapshotOf('serve-root', 'chat-selected');
		expect(snapshot.timeline).toEqual([
			{ offsetSec: 0, label: 'propose' },
			{ offsetSec: 6, label: 'granite' },
			{ offsetSec: 9.5, label: 'qwen' },
			{ offsetSec: 13.3, label: 'gemma' },
			{ offsetSec: 15, label: 'judge' },
			{ offsetSec: 115.5, label: 'done' }
		]);
	});
});

describe('deriveSnapshot: chat run that selected nobody', () => {
	// The score settles a tie, so a run that selects nobody is now the residual
	// case: a judge whose answer no parser could read, or too few candidates.
	it('names the sub-reason in both panels', async () => {
		const snapshot = await snapshotOf('serve-root', 'chat-no-candidate');
		expect(snapshot.select).toMatchObject({
			state: 'no_candidate',
			colour: 'bad',
			text: 'no_candidate (invalid_output)'
		});
		expect(snapshot.judge?.outcome).toEqual({
			state: 'no_candidate',
			text: 'no_candidate (invalid_output)',
			colour: 'bad'
		});
		expect(snapshot.judge?.retries).toBe(1);
		expect(snapshot.judge?.pairs[2].ba.text).toBe('ba invalid 25.9s');
		expect(snapshot.judge?.pairs[2].verdict.text).toBe('draw (invalid)');
		expect(snapshot.judge?.pairs[2].verdict.colour).toBe('bad');
		expect(snapshot.judge?.wins).toEqual([
			{ id: 'granite', wins: 1 },
			{ id: 'qwen', wins: 0 },
			{ id: 'gemma', wins: 1 }
		]);
	});
});

describe('deriveSnapshot: chat run the candidates settled among themselves', () => {
	// judge.json is written with no pairs when a strict majority of the answers
	// agree: the judge was never asked, so the grid has nothing to draw and the
	// outcome line carries the whole story.
	it('draws no judge grid and reports the consensus', async () => {
		const snapshot = await snapshotOf('chat-consensus');
		const judge = snapshot.judge!;
		expect(judge.pairs).toEqual([]);
		expect(judge.latencyText).toBe('0.0s');
		expect(judge.wins).toEqual([
			{ id: 'granite', wins: 0 },
			{ id: 'qwen', wins: 0 },
			{ id: 'gemma', wins: 0 }
		]);
		expect(judge.swapConsistent).toEqual({ pairs: 0, total: 0 });
		expect(judge.outcome).toEqual({
			state: 'selected',
			text: 'selected granite  consensus: 2 of 3 agree on the normalised answ',
			colour: 'ok'
		});
		expect(snapshot.select).toMatchObject({
			state: 'selected',
			colour: 'ok',
			text: 'selected granite',
			kind: 'selected'
		});
		expect(snapshot.select.ranked).toEqual(['granite', 'qwen', 'gemma']);
		expect(snapshot.header.phase).toBe('done');
	});
});

describe('deriveSnapshot: chat run still being judged', () => {
	it('infers the first verdict from the two call files and marks it provisional', async () => {
		const snapshot = await snapshotOf('serve-root', 'chat-judging');
		const judge = snapshot.judge!;
		expect(judge.pairs).toHaveLength(3);
		expect(judge.pairs[0].ab.text).toBe('ab ok A>granite 19.1s');
		expect(judge.pairs[0].ba.text).toBe('ba ok B>granite 13.1s');
		expect(judge.pairs[0].verdict).toEqual({
			state: 'ok',
			text: '~granite',
			colour: 'ok',
			provisional: true
		});
	});

	it('shows the calls that have not landed as running', async () => {
		const snapshot = await snapshotOf('serve-root', 'chat-judging');
		const judge = snapshot.judge!;
		expect(judge.pairs[1].ab).toMatchObject({ state: 'run', text: 'ab running', who: null });
		expect(judge.pairs[1].verdict).toMatchObject({ state: 'run', text: 'judging' });
		expect(judge.seedText).toBe('seed -  nonce - (no judge.json yet)');
		expect(judge.presentation).toBeNull();
		expect(judge.outcome).toEqual({ state: 'pend', text: '-', colour: 'dim' });
		expect(judge.swapConsistent).toBeNull();
	});

	it('waits for select.json and reports the judging phase', async () => {
		const snapshot = await snapshotOf('serve-root', 'chat-judging');
		expect(snapshot.header.phase).toBe('judging');
		expect(snapshot.header.complete).toBe(false);
		expect(snapshot.select).toMatchObject({ state: 'pend', text: 'waiting for select.json...' });
		expect(snapshot.timeline[0]).toEqual({ offsetSec: 0, label: 'propose' });
		expect(snapshot.timeline.map((e) => e.label)).toContain('judge');
	});
});

describe('deriveSnapshot: coding run', () => {
	it('shows the diff summary and the verify status per lane', async () => {
		const snapshot = await snapshotOf('task-coding', 'runs', '20260101T003000Z-44444444');
		expect(snapshot.header.face).toBe('coding');
		expect(snapshot.judge).toBeNull();
		expect(snapshot.lanes.map((l) => l.candidate?.info)).toEqual([
			'1 file(s) +1/-1',
			'1 file(s) +1/-1',
			'1 file(s) +1/-1'
		]);
		expect(snapshot.lanes.map((l) => l.verify?.status)).toEqual(['pass', 'fail', 'pass']);
		expect(snapshot.lanes.map((l) => l.verify?.colour)).toEqual(['ok', 'bad', 'ok']);
		// the coding budget in this run is 1024, not the 512 the chat runs used
		expect(snapshot.lanes[0].maxTokens).toBe(1024);
	});

	it('reports the first passing candidate and the others that passed', async () => {
		const snapshot = await snapshotOf('task-coding', 'runs', '20260101T003000Z-44444444');
		expect(snapshot.select.text).toBe('selected granite');
		expect(snapshot.select.alsoPassed).toEqual(['gemma']);
		expect(snapshot.timeline).toEqual([
			{ offsetSec: 0, label: 'propose' },
			{ offsetSec: 4.6, label: 'granite' },
			{ offsetSec: 7.2, label: 'qwen' },
			{ offsetSec: 11.8, label: 'gemma' },
			{ offsetSec: 20, label: 'verify' },
			{ offsetSec: 30.5, label: 'done' }
		]);
	});

	it('shows a run whose verifier is still going', async () => {
		const snapshot = await snapshotOf('task-coding', 'runs', '20260101T004000Z-55555555');
		expect(snapshot.header.phase).toBe('verifying');
		expect(snapshot.lanes.map((l) => l.verify?.status)).toEqual(['pass', '-', '-']);
		expect(snapshot.lanes[1].verify?.colour).toBe('dim');
		expect(snapshot.select.text).toBe('waiting for select.json...');
		expect(snapshot.timeline.map((e) => e.label)).toEqual([
			'propose',
			'granite',
			'qwen',
			'gemma',
			'verify'
		]);
	});
});

describe('deriveSnapshot: sparse inputs', () => {
	it('shows a run that has only written run.json', async () => {
		const snapshot = await snapshotOf('run-only', '20260101T005000Z-66666666');
		expect(snapshot.header.phase).toBe('proposing');
		expect(snapshot.lanes.map((l) => l.state)).toEqual(['idle', 'idle', 'idle']);
		expect(snapshot.lanes.map((l) => l.note)).toEqual([
			'waiting for candidate...',
			'waiting for candidate...',
			'waiting for candidate...'
		]);
		expect(snapshot.lanes[0].candidate).toBeNull();
		expect(snapshot.timeline).toEqual([{ offsetSec: 0, label: 'propose' }]);
		expect(snapshot.select.text).toBe('waiting for select.json...');
	});

	it('draws the lanes from the config when there is no run at all', () => {
		const snapshot = deriveSnapshot(null, [], config, NOW, 'pinned');
		expect(snapshot.header.runId).toBeNull();
		expect(snapshot.header.phase).toBe('waiting');
		expect(snapshot.header.mode).toBe('pinned');
		expect(snapshot.header.elapsedSec).toBe(0);
		expect(snapshot.lanes.map((l) => l.id)).toEqual(['granite', 'qwen', 'gemma']);
		expect(snapshot.judge).toBeNull();
		expect(snapshot.select.text).toBe('-');
		expect(snapshot.timeline).toEqual([]);
	});
});

describe('deriveSnapshot: live lane states', () => {
	it('is unreachable when the sampler could not reach the server', async () => {
		const files = await filesOf('run-only', '20260101T005000Z-66666666');
		const snapshot = deriveSnapshot(
			files,
			[proposerSample('granite', { reachable: false, baseUrl: 'http://127.0.0.1:8081' })],
			config,
			NOW
		);
		expect(snapshot.lanes[0].state).toBe('unreachable');
		expect(snapshot.lanes[0].colour).toBe('bad');
		expect(snapshot.lanes[0].note).toBe('server unreachable (http://127.0.0.1:8081)');
		// a lane the fleet has no sample for is not evidence of an unreachable server
		expect(snapshot.lanes[1].state).toBe('idle');
		expect(snapshot.lanes[1].live).toBe(false);
	});

	it('is prefill while the prompt is being read and generating once tokens flow', async () => {
		const files = await filesOf('run-only', '20260101T005000Z-66666666');
		const prefill = deriveSnapshot(
			files,
			[proposerSample('granite', { processing: 1, promptTokens: 884, promptProcessed: 300 })],
			config,
			NOW
		);
		expect(prefill.lanes[0].state).toBe('prefill');
		expect(prefill.lanes[0].note).toBe('prefill 300/884 prompt tok');

		const generating = deriveSnapshot(
			files,
			[proposerSample('granite', { processing: 1, decoded: 256, tokPerSec: 12.5 })],
			config,
			NOW
		);
		expect(generating.lanes[0].state).toBe('generating');
		expect(generating.lanes[0].colour).toBe('run');
		expect(generating.lanes[0].barFraction).toBe(0.25); // 256 of the run's 1024
		expect(generating.lanes[0].tokPerSec).toBe(12.5);
	});

	it('caps the bar at a full budget', async () => {
		const files = await filesOf('run-only', '20260101T005000Z-66666666');
		const snapshot = deriveSnapshot(
			files,
			[proposerSample('granite', { processing: 1, decoded: 99_999 })],
			config,
			NOW
		);
		expect(snapshot.lanes[0].barFraction).toBe(1);
	});

	it('measures elapsed against the clock while the run is unfinished', async () => {
		const files = await filesOf('serve-root', 'chat-judging');
		const snapshot = deriveSnapshot(files, [], config, Date.parse('2026-01-01T00:00:30Z'));
		expect(snapshot.header.elapsedSec).toBe(30);
	});
});

describe('provisional verdicts', () => {
	function judging(abWho: string, baWho: string): RunFiles {
		return {
			ref: { id: 'r', dir: '/srv/example/runs/r', root: '/srv/example/runs' },
			run: {
				run_id: 'r',
				created_at: '2026-01-01T00:00:00Z',
				face: 'chat',
				proposers: [{ id: 'a' }, { id: 'b' }]
			},
			task: null,
			conversation: null,
			candidates: { a: { status: 'ok' }, b: { status: 'ok' } },
			answers: {},
			verify: {},
			judgeCalls: {
				'0-ab': { status: 'ok', choice: 'A', choice_candidate: abWho, latency_ms: 1000 },
				'0-ba': { status: 'ok', choice: 'A', choice_candidate: baWho, latency_ms: 1000 }
			},
			judge: null,
			judgeSeedRaw: null,
			select: null,
			verifyDirs: [],
			mtimes: {},
			complete: false,
			readAtMs: 0
		};
	}

	it('is a provisional win when both orders name the same candidate', () => {
		const snapshot = deriveSnapshot(judging('a', 'a'), [], config, NOW);
		expect(snapshot.judge?.pairs[0].verdict).toEqual({
			state: 'ok',
			text: '~a',
			colour: 'ok',
			provisional: true
		});
	});

	it('is a provisional draw when the orders disagree', () => {
		const snapshot = deriveSnapshot(judging('a', 'b'), [], config, NOW);
		expect(snapshot.judge?.pairs[0].verdict.text).toBe('~draw (disagree)');
		expect(snapshot.judge?.pairs[0].verdict.colour).toBe('bad');
	});

	it('is a provisional draw when either order called it a tie', () => {
		const files = judging('a', 'a');
		files.judgeCalls['0-ba'] = { status: 'ok', choice: 'tie', latency_ms: 1000 };
		const snapshot = deriveSnapshot(files, [], config, NOW);
		expect(snapshot.judge?.pairs[0].verdict.text).toBe('~draw (tie)');
	});

	it('reports a call that never parsed and one that timed out', () => {
		const files = judging('a', 'a');
		files.judgeCalls['0-ab'] = { status: 'invalid_output', latency_ms: 2500 };
		files.judgeCalls['0-ba'] = { status: 'timeout', latency_ms: 180000 };
		const snapshot = deriveSnapshot(files, [], config, NOW);
		expect(snapshot.judge?.pairs[0].ab).toMatchObject({
			state: 'invalid',
			text: 'ab invalid 2.5s',
			colour: 'bad'
		});
		expect(snapshot.judge?.pairs[0].ba).toMatchObject({
			state: 'timeout',
			text: 'ba timeout 180.0s',
			colour: 'bad'
		});
		expect(snapshot.judge?.pairs[0].verdict).toMatchObject({ state: 'run', text: 'judging' });
	});
});
