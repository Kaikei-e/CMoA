/**
 * The two pure pieces of the live connection: how a frame off the wire becomes
 * a typed event, and how long to wait before the next attempt. They are here,
 * outside the rune store, so they can be tested without a DOM or a server.
 */
import type { CalibrationState, FleetState, RunSnapshot, RunSummary } from '$lib/types';

/** The first retry waits this long. */
export const BACKOFF_BASE_MS = 1000;
/** No retry ever waits longer than this, however long the server stays away. */
export const BACKOFF_CAP_MS = 15_000;
/** Jitter spreads reconnects by ±25 % so tabs do not stampede a restarted server. */
export const BACKOFF_JITTER = 0.5;

/**
 * `1s, 2s, 4s, 8s, 15s, 15s …`, each multiplied by 0.75–1.25.
 * `random` is injectable so the schedule is exactly assertable.
 */
export function nextDelay(attempt: number, random: () => number = Math.random): number {
	const step = Math.max(0, Math.min(20, Math.floor(attempt)));
	const base = Math.min(BACKOFF_CAP_MS, BACKOFF_BASE_MS * 2 ** step);
	const jitter = 1 + (random() - 0.5) * BACKOFF_JITTER;
	return Math.round(base * jitter);
}

export type MonitorEvent =
	| { kind: 'fleet'; fleet: FleetState }
	| { kind: 'run'; run: RunSnapshot }
	| { kind: 'runs'; runs: RunSummary[] }
	| { kind: 'calibration'; calibration: CalibrationState }
	| { kind: 'error'; message: string };

function isRecord(value: unknown): value is Record<string, unknown> {
	return typeof value === 'object' && value !== null && !Array.isArray(value);
}

/**
 * Turn one named SSE frame into a typed event, or null when it is not one of
 * ours or its payload is the wrong shape. A malformed frame must never take the
 * screen down: the previous state simply stays on it.
 */
export function parseEvent(name: string, data: string): MonitorEvent | null {
	let payload: unknown;
	try {
		payload = JSON.parse(data);
	} catch {
		return null;
	}
	switch (name) {
		case 'fleet':
			return isRecord(payload) && Array.isArray(payload.servers)
				? { kind: 'fleet', fleet: payload as unknown as FleetState }
				: null;
		case 'run':
			return isRecord(payload) && isRecord(payload.header)
				? { kind: 'run', run: payload as unknown as RunSnapshot }
				: null;
		case 'runs':
			return Array.isArray(payload) ? { kind: 'runs', runs: payload as RunSummary[] } : null;
		case 'calibration':
			return isRecord(payload) && Array.isArray(payload.entries)
				? { kind: 'calibration', calibration: payload as unknown as CalibrationState }
				: null;
		case 'error':
			return {
				kind: 'error',
				message:
					isRecord(payload) && typeof payload.message === 'string'
						? payload.message
						: 'the monitor reported an error with no message'
			};
		default:
			return null;
	}
}
