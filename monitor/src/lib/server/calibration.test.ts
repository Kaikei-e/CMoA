import { describe, expect, it } from 'vitest';
import {
	calibrationFromFrontmatter,
	inForceOn,
	loadCalibrations,
	newestPerJudge,
	parseFrontmatter,
	todayString
} from './calibration';
import { fixture } from './testing';

describe('parseFrontmatter', () => {
	it('reads flat scalars, quoted or not', () => {
		const fields = parseFrontmatter(
			['---', 'kind: calibration', 'title: "a: colon inside"', "date: '2026-01-05'", 'n: 12', '---', '', '# body'].join(
				'\n'
			)
		);
		expect(fields).toEqual({
			kind: 'calibration',
			title: 'a: colon inside',
			date: '2026-01-05',
			n: '12'
		});
	});

	it('skips blank lines and comments', () => {
		expect(parseFrontmatter('---\n\n# a comment\njudge: x\n---\n')).toEqual({ judge: 'x' });
	});

	it('is null when the document has no frontmatter', () => {
		expect(parseFrontmatter('# just a heading\n')).toBeNull();
		expect(parseFrontmatter('---\nunterminated: true\n')).toBeNull();
	});
});

describe('inForceOn', () => {
	it('includes the last day and excludes the next', () => {
		expect(inForceOn('2026-02-05', '2026-02-05')).toBe(true);
		expect(inForceOn('2026-02-05', '2026-02-04')).toBe(true);
		expect(inForceOn('2026-02-05', '2026-02-06')).toBe(false);
	});

	it('is false when the document names no end date', () => {
		expect(inForceOn(null, '2026-02-05')).toBe(false);
	});
});

describe('todayString', () => {
	it('formats a local date as YYYY-MM-DD', () => {
		expect(todayString(new Date(2026, 0, 5, 12))).toBe('2026-01-05');
	});
});

describe('calibrationFromFrontmatter', () => {
	it('keeps only kind: calibration', () => {
		expect(calibrationFromFrontmatter('n.md', { kind: 'note' }, '2026-01-06')).toBeNull();
	});

	it('reads the numbers as numbers and leaves absent fields null', () => {
		const entry = calibrationFromFrontmatter(
			'c.md',
			{ kind: 'calibration', judge: 'j', verdict: 'uncalibrated', human_kappa: '0.281' },
			'2026-01-06'
		);
		expect(entry?.humanKappa).toBe(0.281);
		expect(entry?.nHuman).toBeNull();
		expect(entry?.inForce).toBe(false);
	});
});

describe('newestPerJudge', () => {
	it('keeps the newest document for each judge', () => {
		const make = (file: string, judge: string, date: string) =>
			calibrationFromFrontmatter(file, { kind: 'calibration', judge, date }, '2026-01-06')!;
		const kept = newestPerJudge([
			make('old.md', 'j', '2025-12-01'),
			make('new.md', 'j', '2026-01-05'),
			make('other.md', 'k', '2024-01-01')
		]);
		expect(kept.map((c) => c.file)).toEqual(['new.md', 'other.md']);
	});
});

describe('loadCalibrations', () => {
	it('reads the directory and keeps one document per judge', async () => {
		const state = await loadCalibrations(fixture('calibrations'), '2026-01-06');
		expect(state.entries.map((c) => c.file)).toEqual([
			'gpt-oss-20b@2026-01-05.md',
			'other-judge@2026-01-04.md'
		]);
		const judge = state.entries[0];
		expect(judge).toMatchObject({
			judge: 'gpt-oss-20b',
			verdict: 'uncalibrated',
			tieHandling: 'abstain-as-category',
			humanKappa: 0.281,
			nHuman: 200,
			swapKappa: 0.589,
			rerunKappa: 0.82,
			inForceUntil: '2026-02-05',
			inForce: true
		});
		expect(judge.title).toBe('suite-chat judged by gpt-oss-20b: uncalibrated');
	});

	it('computes inForce against the day it is asked about', async () => {
		const later = await loadCalibrations(fixture('calibrations'), '2026-03-02');
		expect(later.entries.map((c) => c.inForce)).toEqual([false, false]);
	});

	it('reports an empty list when the directory is missing or unset', async () => {
		expect(await loadCalibrations(null)).toEqual({ dir: null, entries: [] });
		const missing = await loadCalibrations(fixture('no-such-dir'), '2026-01-06');
		expect(missing.entries).toEqual([]);
		expect(missing.dir).toBe(fixture('no-such-dir'));
	});
});
