import type { RequestHandler } from './$types';
import { jsonResponse, monitorOrError } from '$lib/server/http';

/** The run history, newest first. */
export const GET: RequestHandler = async () => {
	const found = monitorOrError();
	if ('response' in found) return found.response;
	return jsonResponse({ runs: await found.monitor.listRuns() });
};
