import { json } from '@sveltejs/kit';
import { getMonitor, type Monitor } from './stream';

/** Every monitor response is live data; nothing on this server may be cached. */
export const NO_STORE = { 'cache-control': 'no-store, no-transform' } as const;

export function jsonResponse(data: unknown, status = 200): Response {
	return json(data, { status, headers: NO_STORE });
}

/**
 * The monitor, or the response explaining why there is none. A bad environment
 * is a 503 rather than a crash: the page can then say so on screen.
 */
export function monitorOrError(): { monitor: Monitor } | { response: Response } {
	try {
		return { monitor: getMonitor() };
	} catch (err) {
		return { response: jsonResponse({ error: (err as Error).message }, 503) };
	}
}
