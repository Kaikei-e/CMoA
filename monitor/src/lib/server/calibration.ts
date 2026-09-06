import { readdir, readFile } from 'node:fs/promises';
import { join } from 'node:path';
import type { Calibration, CalibrationState } from '$lib/types';

/**
 * Parse the flat YAML frontmatter a calibration document carries: a `---`
 * fence at the very top, `key: value` lines, values optionally quoted. Nothing
 * nested is used by these documents, so no YAML library is pulled in for it.
 * Returns null when the file has no frontmatter at all.
 */
export function parseFrontmatter(text: string): Record<string, string> | null {
	const body = text.replace(/^\uFEFF/, '');
	if (!/^---\r?\n/.test(body)) return null;
	const end = body.indexOf('\n---', 3);
	if (end < 0) return null;
	const fields: Record<string, string> = {};
	for (const line of body.slice(4, end).split('\n')) {
		const trimmed = line.trim();
		if (trimmed === '' || trimmed.startsWith('#')) continue;
		const sep = trimmed.indexOf(':');
		if (sep <= 0) continue;
		const key = trimmed.slice(0, sep).trim();
		let value = trimmed.slice(sep + 1).trim();
		if (value.length >= 2 && /^(".*"|'.*')$/s.test(value)) value = value.slice(1, -1);
		fields[key] = value;
	}
	return fields;
}

function num(value: string | undefined): number | null {
	if (value === undefined || value.trim() === '') return null;
	const n = Number(value);
	return Number.isFinite(n) ? n : null;
}

function str(value: string | undefined): string | null {
	return value === undefined || value.trim() === '' ? null : value;
}

/**
 * A calibration is in force through `in_force_until` inclusive. A document with
 * no such date is never in force: an unbounded calibration is a claim nobody made.
 */
export function inForceOn(inForceUntil: string | null, today: string): boolean {
	return inForceUntil !== null && today <= inForceUntil;
}

/** Today as `YYYY-MM-DD`, in the local zone the operator reads dates in. */
export function todayString(now: Date = new Date()): string {
	const pad = (n: number) => String(n).padStart(2, '0');
	return `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())}`;
}

export function calibrationFromFrontmatter(
	file: string,
	fields: Record<string, string>,
	today: string
): Calibration | null {
	if (fields.kind !== 'calibration') return null;
	const inForceUntil = str(fields.in_force_until);
	return {
		file,
		id: fields.id ?? file,
		title: fields.title ?? '',
		judge: fields.judge ?? '?',
		verdict: fields.verdict ?? '?',
		date: str(fields.date),
		pool: str(fields.pool),
		windowFrom: str(fields.window_from),
		windowTo: str(fields.window_to),
		inForceUntil,
		tieHandling: str(fields.tie_handling),
		humanKappa: num(fields.human_kappa),
		nHuman: num(fields.n_human),
		swapKappa: num(fields.swap_kappa),
		rerunKappa: num(fields.rerun_kappa),
		report: str(fields.report),
		inForce: inForceOn(inForceUntil, today)
	};
}

/** The newest calibration per judge, by `date` and then by file name. */
export function newestPerJudge(entries: Calibration[]): Calibration[] {
	const best = new Map<string, Calibration>();
	for (const entry of entries) {
		const current = best.get(entry.judge);
		if (!current) {
			best.set(entry.judge, entry);
			continue;
		}
		const a = `${entry.date ?? ''}|${entry.file}`;
		const b = `${current.date ?? ''}|${current.file}`;
		if (a > b) best.set(entry.judge, entry);
	}
	return [...best.values()].sort((a, b) => a.judge.localeCompare(b.judge));
}

/**
 * Read every `*.md` in the calibrations directory. A missing directory is not an
 * error: the panel then says there is no calibration on file, which is itself
 * the finding.
 */
export async function loadCalibrations(
	dir: string | null,
	today: string = todayString()
): Promise<CalibrationState> {
	if (!dir) return { dir: null, entries: [] };
	let names: string[];
	try {
		names = (await readdir(dir)).filter((name) => name.endsWith('.md')).sort();
	} catch {
		return { dir, entries: [] };
	}
	const entries: Calibration[] = [];
	for (const name of names) {
		let text: string;
		try {
			text = await readFile(join(dir, name), 'utf8');
		} catch {
			continue;
		}
		const fields = parseFrontmatter(text);
		if (!fields) continue;
		const entry = calibrationFromFrontmatter(name, fields, today);
		if (entry) entries.push(entry);
	}
	return { dir, entries: newestPerJudge(entries) };
}
