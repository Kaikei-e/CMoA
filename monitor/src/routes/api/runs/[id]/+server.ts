import type { RequestHandler } from './$types';
import { jsonResponse, monitorOrError } from '$lib/server/http';

/** One run's snapshot, the same object the `run` SSE event carries. */
export const GET: RequestHandler = async ({ params }) => {
	const found = monitorOrError();
	if ('response' in found) return found.response;
	const monitor = found.monitor;

	const ref = await monitor.resolveRef(params.id);
	if (!ref) return jsonResponse({ error: `no run ${params.id} under the monitored roots` }, 404);
	return jsonResponse(await monitor.snapshotFor(ref.id));
};
