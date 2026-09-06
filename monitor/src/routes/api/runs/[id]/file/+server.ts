import type { RequestHandler } from './$types';
import { jsonResponse, monitorOrError } from '$lib/server/http';
import { readTraceFile } from '$lib/server/files';

/**
 * One whitelisted trace file, as text. `?path=` is relative to the run
 * directory, except `conversation.json`, which is read from the task or
 * serve-request directory beside it.
 */
export const GET: RequestHandler = async ({ params, url }) => {
	const found = monitorOrError();
	if ('response' in found) return found.response;
	const monitor = found.monitor;

	const relPath = url.searchParams.get('path') ?? '';
	const ref = await monitor.resolveRef(params.id);
	if (!ref) return jsonResponse({ error: `no run ${params.id} under the monitored roots` }, 404);

	const result = await readTraceFile(ref, relPath);
	if (!result.ok) {
		return result.reason === 'rejected'
			? jsonResponse({ error: `path not allowed: ${relPath}` }, 400)
			: jsonResponse({ error: `no such file: ${relPath}` }, 404);
	}
	return jsonResponse({ run: ref.id, ...result.file });
};
