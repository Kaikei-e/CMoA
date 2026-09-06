import type { Colour, FleetSample } from '$lib/types';

/** The class that paints a precomputed `colour`. Never derive a colour from a word. */
export function tone(colour: Colour | null | undefined): string {
	switch (colour) {
		case 'ok':
			return 'c-ok';
		case 'run':
			return 'c-run';
		case 'bad':
			return 'c-bad';
		default:
			return 'c-dim';
	}
}

/** `13.3` — one decimal, tabular, never `13.30000000000001`. */
export function oneDecimal(value: number): string {
	if (!Number.isFinite(value)) return '0.0';
	return value.toFixed(1);
}

/** `115.5s`, or `1:55.5` once a round runs past a minute. */
export function clock(seconds: number): string {
	if (!Number.isFinite(seconds) || seconds < 0) return '0.0s';
	if (seconds < 60) return `${seconds.toFixed(1)}s`;
	const minutes = Math.floor(seconds / 60);
	const rest = seconds - minutes * 60;
	return `${minutes}:${rest < 10 ? '0' : ''}${rest.toFixed(1)}`;
}

/**
 * Milliseconds from an RFC 3339 stamp, tolerating the nanosecond precision the
 * Go writer emits. Returns null rather than NaN so callers can branch.
 */
export function stampMs(stamp: string | null | undefined): number | null {
	if (!stamp) return null;
	const trimmed = stamp.replace(/(\.\d{3})\d+/, '$1');
	const ms = Date.parse(trimmed);
	return Number.isFinite(ms) ? ms : null;
}

/** `20260101T000000Z-11111111` → `00:00:00Z`, for a history row. */
export function shortTime(stamp: string | null | undefined): string {
	const ms = stampMs(stamp);
	if (ms === null) return '--:--:--';
	return new Date(ms).toISOString().slice(11, 19) + 'Z';
}

/** The last path segment, so a fixture root never prints an absolute path. */
export function basename(path: string | null | undefined): string {
	if (!path) return '-';
	const parts = path.split('/').filter(Boolean);
	return parts.length ? parts[parts.length - 1] : path;
}

/** `2621440` → `2.5 MiB`; used for the inspected file's size. */
export function bytes(count: number): string {
	if (count < 1024) return `${count} B`;
	if (count < 1024 * 1024) return `${(count / 1024).toFixed(1)} KiB`;
	return `${(count / (1024 * 1024)).toFixed(1)} MiB`;
}

export interface FleetCellState {
	word: string;
	colour: Colour;
	/** `300/884` while the prompt is being read, else null. */
	prefill: { done: number; total: number; fraction: number } | null;
}

/**
 * The state word for a fleet cell. The lane rows get theirs from the server,
 * but the fleet band is live even when no run is on screen, so this one word
 * is derived here — from `/slots` alone, exactly as the lane rule does it.
 */
export function fleetCellState(sample: FleetSample | null | undefined): FleetCellState {
	if (!sample || !sample.reachable) return { word: 'unreachable', colour: 'bad', prefill: null };
	if (sample.processing <= 0) return { word: 'idle', colour: 'dim', prefill: null };
	if (sample.decoded > 0) return { word: 'generating', colour: 'run', prefill: null };
	const total = sample.promptTokens;
	return {
		word: 'prefill',
		colour: 'run',
		prefill:
			total > 0
				? {
						done: sample.promptProcessed,
						total,
						fraction: Math.max(0, Math.min(1, sample.promptProcessed / total))
					}
				: null
	};
}

/** `http://127.0.0.1:8081` → `127.0.0.1:8081`, which is what an operator reads. */
export function hostOf(baseUrl: string): string {
	return baseUrl.replace(/^https?:\/\//, '').replace(/\/+$/, '');
}

/** Pretty-print a JSON body, leaving anything that is not JSON untouched. */
export function prettyJson(path: string, content: string): string {
	if (!path.endsWith('.json')) return content;
	try {
		return JSON.stringify(JSON.parse(content), null, 2);
	} catch {
		// a truncated body is not parseable; showing the raw head is the point
		return content;
	}
}
