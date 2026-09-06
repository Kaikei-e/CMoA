import { beforeEach, describe, expect, it } from 'vitest';
import {
	clearRunCache,
	discoverRuns,
	lastUserMessage,
	latestRun,
	rawPresentationSeed,
	readRunFiles,
	summariseRun
} from './runs';
import { fixture } from './testing';
import { mkdtemp, mkdir, writeFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const SERVE_ROOT = fixture('serve-root');
const TASK_DIR = fixture('task-coding');
const RUN_DIR = fixture('run-only', '20260101T005000Z-66666666');
const EMPTY_TASK = fixture('empty-task');

beforeEach(() => {
	clearRunCache();
});

describe('discoverRuns', () => {
	it('walks a serve root, whose children each hold runs/', async () => {
		const refs = await discoverRuns([SERVE_ROOT]);
		expect(refs.map((r) => r.id)).toEqual([
			'20260101T002000Z-33333333',
			'20260101T001000Z-22222222',
			'20260101T000000Z-11111111'
		]);
		expect(refs[2].requestDir).toBe(fixture('serve-root', 'chat-selected'));
		expect(refs[2].root).toBe(SERVE_ROOT);
	});

	it('walks a task directory', async () => {
		const refs = await discoverRuns([TASK_DIR]);
		expect(refs.map((r) => r.id)).toEqual([
			'20260101T004000Z-55555555',
			'20260101T003000Z-44444444'
		]);
		// a coding task has no conversation, so there is no request directory
		expect(refs[0].requestDir).toBeUndefined();
	});

	it('accepts a run directory as a root', async () => {
		const refs = await discoverRuns([RUN_DIR]);
		expect(refs.map((r) => r.id)).toEqual(['20260101T005000Z-66666666']);
		expect(refs[0].dir).toBe(RUN_DIR);
	});

	it('skips dot-prefixed names, which are writes still in progress', async () => {
		const dir = await mkdtemp(join(tmpdir(), 'cmoa-monitor-'));
		try {
			await mkdir(join(dir, 'runs', '20260101T009000Z-aaaaaaaa'), { recursive: true });
			await writeFile(join(dir, 'runs', '20260101T009000Z-aaaaaaaa', 'run.json'), '{}');
			await mkdir(join(dir, 'runs', '.20260101T009100Z-bbbbbbbb'), { recursive: true });
			await writeFile(join(dir, 'runs', '.20260101T009100Z-bbbbbbbb', 'run.json'), '{}');
			const refs = await discoverRuns([dir]);
			expect(refs.map((r) => r.id)).toEqual(['20260101T009000Z-aaaaaaaa']);
		} finally {
			await rm(dir, { recursive: true, force: true });
		}
	});

	it('finds nothing in an empty task directory and does not throw', async () => {
		expect(await discoverRuns([EMPTY_TASK, fixture('does-not-exist')])).toEqual([]);
	});

	it('sorts every root together, newest first', async () => {
		const refs = await discoverRuns([TASK_DIR, SERVE_ROOT, RUN_DIR]);
		expect(refs.map((r) => r.id)).toEqual([
			'20260101T005000Z-66666666',
			'20260101T004000Z-55555555',
			'20260101T003000Z-44444444',
			'20260101T002000Z-33333333',
			'20260101T001000Z-22222222',
			'20260101T000000Z-11111111'
		]);
		expect(latestRun(refs)?.id).toBe('20260101T005000Z-66666666');
		expect(latestRun([])).toBeNull();
	});
});

describe('readRunFiles', () => {
	it('reads a finished chat run whole', async () => {
		const [ref] = await discoverRuns([fixture('serve-root', 'chat-selected')]);
		const files = await readRunFiles(ref);
		expect(Object.keys(files.candidates).sort()).toEqual(['gemma', 'granite', 'qwen']);
		expect(files.answers.gemma).toContain('Candidate answer placeholder for gemma');
		expect(Object.keys(files.judgeCalls).sort()).toEqual([
			'0-ab',
			'0-ba',
			'1-ab',
			'1-ba',
			'2-ab',
			'2-ba'
		]);
		expect(files.judge?.wins).toEqual({ gemma: 2, granite: 0, qwen: 0 });
		expect(files.select?.selection?.candidate_id).toBe('gemma');
		expect(files.conversation?.length).toBe(1);
		expect(files.complete).toBe(true);
		expect(files.mtimes['select.json']).toBeGreaterThan(0);
	});

	it('caches a run that has written select.json', async () => {
		const [ref] = await discoverRuns([fixture('serve-root', 'chat-selected')]);
		const first = await readRunFiles(ref);
		expect(await readRunFiles(ref)).toBe(first);
	});

	it('does not cache a run that is still going', async () => {
		const [ref] = await discoverRuns([fixture('serve-root', 'chat-judging')]);
		const first = await readRunFiles(ref);
		expect(first.complete).toBe(false);
		expect(await readRunFiles(ref)).not.toBe(first);
	});

	it('tolerates a run that holds nothing but run.json', async () => {
		const [ref] = await discoverRuns([RUN_DIR]);
		const files = await readRunFiles(ref);
		expect(files.run?.face).toBe('coding');
		expect(files.candidates).toEqual({});
		expect(files.judge).toBeNull();
		expect(files.select).toBeNull();
		expect(files.verifyDirs).toEqual([]);
	});

	it('records a verify directory that has no result.json yet', async () => {
		const [ref] = await discoverRuns([fixture('task-coding', 'runs', '20260101T004000Z-55555555')]);
		const files = await readRunFiles(ref);
		expect(files.verifyDirs.sort()).toEqual(['granite', 'qwen']);
		expect(files.verify.granite.status).toBe('pass');
		expect(files.verify.qwen).toBeUndefined();
	});
});

describe('summariseRun', () => {
	it('previews a chat run by its last user message', async () => {
		const [ref] = await discoverRuns([fixture('serve-root', 'chat-selected')]);
		const summary = summariseRun(await readRunFiles(ref));
		expect(summary.face).toBe('chat');
		expect(summary.outcome).toBe('selected gemma');
		expect(summary.outcomeState).toBe('selected');
		expect(summary.outcomeColour).toBe('ok');
		expect(summary.preview.startsWith('Explain the difference between a nil slice')).toBe(true);
		expect(summary.preview.length).toBeLessThanOrEqual(80);
	});

	it('names the sub-reason of a run that selected nobody', async () => {
		const [ref] = await discoverRuns([fixture('serve-root', 'chat-no-candidate')]);
		const summary = summariseRun(await readRunFiles(ref));
		expect(summary.outcome).toBe('no_candidate (invalid_output)');
		expect(summary.outcomeColour).toBe('bad');
	});

	it('previews a coding run by its task id and reports it as in progress', async () => {
		const [ref] = await discoverRuns([fixture('task-coding', 'runs', '20260101T004000Z-55555555')]);
		const summary = summariseRun(await readRunFiles(ref));
		expect(summary.face).toBe('coding');
		expect(summary.preview).toBe('hello');
		expect(summary.outcome).toBe('in progress');
		expect(summary.outcomeColour).toBe('run');
	});
});

describe('rawPresentationSeed', () => {
	it('keeps an int64 seed exactly, which JSON.parse would round', () => {
		const text = '{"presentation": {"seed": -7536333114186850973, "seed_source": "run_id"}}';
		expect(rawPresentationSeed(text)).toBe('-7536333114186850973');
		expect(String(JSON.parse(text).presentation.seed)).not.toBe('-7536333114186850973');
	});

	it('is null when the document carries no presentation seed', () => {
		expect(rawPresentationSeed('{"pairs": []}')).toBeNull();
	});

	it('reads the seed of a fixture run through readRunFiles', async () => {
		const [ref] = await discoverRuns([fixture('serve-root', 'chat-selected')]);
		expect((await readRunFiles(ref)).judgeSeedRaw).toBe('7');
	});
});

describe('lastUserMessage', () => {
	it('takes the last user turn, not the last turn', () => {
		expect(
			lastUserMessage([
				{ role: 'user', content: 'first' },
				{ role: 'assistant', content: 'reply' },
				{ role: 'user', content: 'second' }
			])
		).toBe('second');
		expect(lastUserMessage([{ role: 'assistant', content: 'only' }])).toBeNull();
		expect(lastUserMessage(null)).toBeNull();
	});
});
