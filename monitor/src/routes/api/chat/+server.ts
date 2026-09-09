import type { RequestHandler } from './$types';
import type { ServeConfig } from '$lib/types';
import { relayChat } from '$lib/server/chat';
import { getMonitor } from '$lib/server/stream';

/**
 * The one route that is not read-only, and it still writes nothing itself:
 * it forwards a conversation to `cmoa serve`, which runs the round and writes
 * the trace the rest of the screen reads.
 */
export const POST: RequestHandler = async ({ request }) => {
	let serve: ServeConfig | null = null;
	let configError: string | null = null;
	let gateway = '';
	try {
		const monitor = getMonitor();
		serve = monitor.env.config.serve ?? null;
		gateway = monitor.env.gateway;
	} catch (err) {
		configError = (err as Error).message;
	}
	return relayChat(request, { serve, configError, gateway });
};
