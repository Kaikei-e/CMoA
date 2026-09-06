import type { RequestHandler } from './$types';
import { monitorOrError, NO_STORE } from '$lib/server/http';

/**
 * The live feed. `?run=<id>` pins one run; without it the stream follows the
 * newest run under the monitored roots and switches when a newer one appears.
 *
 * Events: `fleet` every tick, `run` / `runs` / `calibration` when they change,
 * `error` when the run cannot be derived. `: keep-alive` comments hold the
 * connection open through proxies during a quiet round.
 */
export const GET: RequestHandler = ({ url, request }) => {
	const found = monitorOrError();
	if ('response' in found) return found.response;
	const monitor = found.monitor;

	const runId = url.searchParams.get('run');
	const encoder = new TextEncoder();
	let unsubscribe: (() => void) | null = null;

	const stream = new ReadableStream<Uint8Array>({
		start(controller) {
			let closed = false;
			const write = (chunk: string) => {
				if (closed) return;
				try {
					controller.enqueue(encoder.encode(chunk));
				} catch {
					closed = true;
				}
			};
			const close = () => {
				if (closed) return;
				closed = true;
				unsubscribe?.();
				unsubscribe = null;
				try {
					controller.close();
				} catch {
					// the client hung up first
				}
			};

			write(': cmoa-monitor\n\n');
			unsubscribe = monitor.subscribe(
				runId && runId.trim() !== '' ? runId.trim() : null,
				(event, data) => write(`event: ${event}\ndata: ${JSON.stringify(data)}\n\n`),
				() => write(': keep-alive\n\n')
			);

			// The browser going away is the only way this stream ends.
			request.signal.addEventListener('abort', close);
			if (request.signal.aborted) close();
		},
		cancel() {
			unsubscribe?.();
			unsubscribe = null;
		}
	});

	return new Response(stream, {
		headers: {
			...NO_STORE,
			'content-type': 'text/event-stream',
			// nginx and friends buffer event streams into uselessness without this.
			'x-accel-buffering': 'no'
		}
	});
};
