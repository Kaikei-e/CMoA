import type { ServerInit } from '@sveltejs/kit';
import { startMonitor } from '$lib/server/stream';

/** One polling loop per process, started before the first request is served. */
export const init: ServerInit = () => {
	startMonitor();
};
