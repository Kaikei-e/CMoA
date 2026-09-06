import type { RequestHandler } from './$types';
import { jsonResponse, monitorOrError } from '$lib/server/http';

/** The newest calibration on file per judge, with `inForce` against today. */
export const GET: RequestHandler = async () => {
	const found = monitorOrError();
	if ('response' in found) return found.response;
	return jsonResponse(await found.monitor.calibrationState());
};
