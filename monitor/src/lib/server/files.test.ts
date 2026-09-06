import { describe, expect, it } from 'vitest';
import { isAllowedTracePath, readTraceFile, traceBaseOf } from './files';
import { discoverRuns } from './runs';
import { fixture } from './testing';

async function chatRun() {
	const [ref] = await discoverRuns([fixture('serve-root', 'chat-selected')]);
	return ref;
}

describe('the trace path allowlist', () => {
	it('accepts every path the display links to', () => {
		for (const path of [
			'run.json',
			'select.json',
			'judge.json',
			'candidates/granite.json',
			'candidates/granite.txt',
			'candidates/granite.raw.txt',
			'candidates/granite.diff',
			'prompt/granite.json',
			'verify/granite/result.json',
			'verify/granite/stdout.txt',
			'verify/granite/stderr.txt',
			'judge/0-ab.json',
			'judge/12-ba.json'
		]) {
			expect(isAllowedTracePath(path)).toBe(true);
		}
	});

	it('roots conversation.json at the request directory', () => {
		expect(traceBaseOf('conversation.json')).toBe('request');
		expect(traceBaseOf('run.json')).toBe('run');
	});

	it('rejects traversal in every spelling', () => {
		for (const path of [
			'../run.json',
			'candidates/../../run.json',
			'candidates/../../../etc/passwd',
			'/etc/passwd',
			'candidates\\granite.txt',
			'..',
			''
		]) {
			expect(isAllowedTracePath(path)).toBe(false);
		}
	});

	it('rejects paths that are simply not trace', () => {
		for (const path of [
			'task.json',
			'notes.md',
			'candidates/granite.png',
			'candidates/granite',
			'judge/x-ab.json',
			'judge/0-cd.json',
			'verify/granite/other.txt',
			'prompt/granite.txt'
		]) {
			expect(isAllowedTracePath(path)).toBe(false);
		}
	});
});

describe('readTraceFile', () => {
	it('reads a whitelisted file out of the run directory', async () => {
		const result = await readTraceFile(await chatRun(), 'candidates/gemma.txt');
		expect(result.ok).toBe(true);
		if (!result.ok) return;
		expect(result.file.content).toContain('Candidate answer placeholder for gemma');
		expect(result.file.truncated).toBe(false);
		expect(result.file.bytes).toBe(result.file.content.length);
	});

	it('reads conversation.json out of the request directory beside the run', async () => {
		const result = await readTraceFile(await chatRun(), 'conversation.json');
		expect(result.ok).toBe(true);
		if (!result.ok) return;
		expect(JSON.parse(result.file.content)[0].role).toBe('user');
	});

	it('cuts a body at the cap and says so', async () => {
		const result = await readTraceFile(await chatRun(), 'candidates/gemma.txt', 16);
		expect(result.ok).toBe(true);
		if (!result.ok) return;
		expect(result.file.content.length).toBe(16);
		expect(result.file.truncated).toBe(true);
		expect(result.file.bytes).toBeGreaterThan(16);
	});

	it('rejects a path off the allowlist without touching the disk', async () => {
		expect(await readTraceFile(await chatRun(), '../../../etc/passwd')).toEqual({
			ok: false,
			reason: 'rejected'
		});
		expect(await readTraceFile(await chatRun(), 'task.json')).toEqual({
			ok: false,
			reason: 'rejected'
		});
	});

	it('reports an allowed file that is not there as missing', async () => {
		expect(await readTraceFile(await chatRun(), 'verify/granite/result.json')).toEqual({
			ok: false,
			reason: 'missing'
		});
	});

	it('has nowhere to read conversation.json from without a request directory', async () => {
		const [ref] = await discoverRuns([fixture('task-coding')]);
		expect(await readTraceFile(ref, 'conversation.json')).toEqual({
			ok: false,
			reason: 'missing'
		});
	});
});
