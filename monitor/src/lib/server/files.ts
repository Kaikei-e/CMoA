import { readFile, stat } from 'node:fs/promises';
import { resolve, sep } from 'node:path';
import type { RunRef, TraceFileResult } from '$lib/types';

/** Bodies are cut here; the drawer shows the head and says it was cut. */
export const MAX_TRACE_BYTES = 256 * 1024;

/** A proposer id, a verify directory name: the alphabet CMoA itself enforces. */
const ID = '[A-Za-z0-9][A-Za-z0-9._-]{0,63}';

/**
 * The only paths a run directory will hand out. Everything the monitor displays
 * is on this list and nothing else is: an allowlist is the whole defence, since
 * a run directory sits inside a working tree full of files that are not trace.
 */
const RUN_PATHS: RegExp[] = [
	/^run\.json$/,
	/^select\.json$/,
	/^judge\.json$/,
	new RegExp(`^candidates/${ID}\\.(txt|diff|raw\\.txt|json)$`),
	new RegExp(`^prompt/${ID}\\.json$`),
	new RegExp(`^verify/${ID}/(stdout|stderr)\\.txt$`),
	new RegExp(`^verify/${ID}/result\\.json$`),
	/^judge\/\d{1,4}-(ab|ba)\.json$/
];

/** The one path served out of the task or serve-request directory. */
const REQUEST_PATHS: RegExp[] = [/^conversation\.json$/];

export type TraceBase = 'run' | 'request';

/** Which directory a whitelisted path is rooted at, or null when it is not on the list. */
export function traceBaseOf(relPath: string): TraceBase | null {
	if (relPath === '' || relPath.includes('\0')) return null;
	// Normalising first would let `candidates/../../x` pass as `x`; the raw path
	// must match the allowlist, and the resolve below is the second gate.
	if (relPath.includes('\\') || relPath.startsWith('/') || relPath.split('/').includes('..'))
		return null;
	if (RUN_PATHS.some((pattern) => pattern.test(relPath))) return 'run';
	if (REQUEST_PATHS.some((pattern) => pattern.test(relPath))) return 'request';
	return null;
}

export function isAllowedTracePath(relPath: string): boolean {
	return traceBaseOf(relPath) !== null;
}

function within(base: string, target: string): boolean {
	const root = resolve(base);
	const path = resolve(target);
	return path === root || path.startsWith(root + sep);
}

/**
 * Read one whitelisted file out of a run. `rejected` means the path is not on
 * the allowlist or escapes its directory; `missing` means it is not there yet,
 * which for a live run is the normal case.
 */
export async function readTraceFile(
	ref: RunRef,
	relPath: string,
	maxBytes: number = MAX_TRACE_BYTES
): Promise<TraceFileResult> {
	const base = traceBaseOf(relPath);
	if (base === null) return { ok: false, reason: 'rejected' };
	const dir = base === 'request' ? ref.requestDir : ref.dir;
	if (!dir) return { ok: false, reason: 'missing' };

	const target = resolve(dir, relPath);
	if (!within(dir, target)) return { ok: false, reason: 'rejected' };

	let bytes: number;
	try {
		const info = await stat(target);
		if (!info.isFile()) return { ok: false, reason: 'rejected' };
		bytes = info.size;
	} catch {
		return { ok: false, reason: 'missing' };
	}

	let content: string;
	try {
		const buffer = await readFile(target);
		content = buffer.subarray(0, maxBytes).toString('utf8');
	} catch {
		return { ok: false, reason: 'missing' };
	}

	return {
		ok: true,
		file: { relPath, bytes, truncated: bytes > maxBytes, content }
	};
}
