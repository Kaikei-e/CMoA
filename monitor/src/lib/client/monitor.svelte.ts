import { goto } from '$app/navigation';
import type { CalibrationState, FleetState, RunSnapshot, RunSummary } from '$lib/types';
import { nextDelay, parseEvent } from './protocol';

export type LinkState = 'connecting' | 'live' | 'lost';
export type DrawerName = 'runs' | 'inspect' | null;

/** One inspected trace file, as the drawer needs it. */
export interface InspectState {
	path: string | null;
	loading: boolean;
	error: string | null;
	bytes: number;
	truncated: boolean;
	content: string;
}

const MOTION_KEY = 'cmoa-monitor:motion';

/** Storage is a convenience, never a dependency: a private window must still work. */
function readStored(key: string): string | null {
	try {
		return globalThis.sessionStorage?.getItem(key) ?? null;
	} catch {
		return null;
	}
}

function writeStored(key: string, value: string): void {
	try {
		globalThis.sessionStorage?.setItem(key, value);
	} catch {
		// nothing to do: the preference simply does not survive the tab
	}
}

/**
 * Everything the screen reads, and the one connection that fills it.
 *
 * The snapshots arrive whole on every tick, so they are `$state.raw`: wrapping
 * a document that is about to be replaced in a deep proxy costs work no one
 * benefits from. Only the small UI-local flags are deeply reactive.
 */
export class MonitorStore {
	fleet = $state.raw<FleetState | null>(null);
	run = $state.raw<RunSnapshot | null>(null);
	runs = $state.raw<RunSummary[]>([]);
	calibration = $state.raw<CalibrationState | null>(null);

	link = $state<LinkState>('connecting');
	/** A fault the screen must show instead of pretending it has data. */
	error = $state<string | null>(null);
	/** The run id in the URL, or null while following the newest run. */
	pinnedId = $state<string | null>(null);

	/** Local clock, so elapsed advances between `run` events. */
	nowMs = $state(Date.now());
	/** Observed distance between fleet samples, for the status bar. */
	intervalMs = $state(0);

	drawer = $state<DrawerName>(null);
	inspect = $state<InspectState>({
		path: null,
		loading: false,
		error: null,
		bytes: 0,
		truncated: false,
		content: ''
	});

	motion = $state(readStored(MOTION_KEY) !== 'off');

	#source: EventSource | null = null;
	#retry: ReturnType<typeof setTimeout> | null = null;
	#attempt = 0;
	#url: string | null = null;
	#lastSampleMs = 0;
	#inspectSeq = 0;

	/** Seconds since `created_at`, ticking locally while the run is unfinished. */
	get elapsedSec(): number {
		const header = this.run?.header;
		if (!header) return 0;
		if (header.complete || !header.createdAt) return header.elapsedSec;
		const started = Date.parse(header.createdAt.replace(/(\.\d{3})\d+/, '$1'));
		if (!Number.isFinite(started)) return header.elapsedSec;
		return Math.max(header.elapsedSec, (this.nowMs - started) / 1000);
	}

	/** Open (or re-open) the stream for a run id. Calling it twice for the same id is free. */
	connect(runId: string | null): void {
		const url = `/api/stream${runId ? `?run=${encodeURIComponent(runId)}` : ''}`;
		this.pinnedId = runId;
		if (this.#url === url && this.#source) return;
		this.#url = url;
		this.#attempt = 0;
		this.#open();
	}

	disconnect(): void {
		if (this.#retry) clearTimeout(this.#retry);
		this.#retry = null;
		this.#source?.close();
		this.#source = null;
	}

	#open(): void {
		if (typeof EventSource === 'undefined' || this.#url === null) return;
		this.#source?.close();
		// Only the very first attempt is `connecting`; once a stream has dropped the
		// lamp stays on LOST until one actually opens, instead of flickering.
		if (this.#attempt === 0 && this.link !== 'live') this.link = 'connecting';

		const source = new EventSource(this.#url);
		this.#source = source;

		const receive = (name: string) => (event: MessageEvent) => {
			// A frame from a stream we already abandoned must not repaint the screen.
			if (this.#source !== source) return;
			this.#apply(name, event.data);
		};
		for (const name of ['fleet', 'run', 'runs', 'calibration', 'error'] as const) {
			source.addEventListener(name, receive(name) as EventListener);
		}
		source.onopen = () => {
			if (this.#source !== source) return;
			this.link = 'live';
			this.#attempt = 0;
		};
		source.onerror = () => {
			if (this.#source !== source) return;
			this.#fail();
		};
	}

	#apply(name: string, data: string): void {
		const event = parseEvent(name, data);
		if (!event) return;
		switch (event.kind) {
			case 'fleet': {
				const sampled = event.fleet.sampledAtMs;
				if (this.#lastSampleMs > 0 && sampled > this.#lastSampleMs) {
					this.intervalMs = sampled - this.#lastSampleMs;
				}
				this.#lastSampleMs = sampled;
				this.fleet = event.fleet;
				this.link = 'live';
				this.error = null;
				this.#attempt = 0;
				break;
			}
			case 'run':
				this.run = event.run;
				break;
			case 'runs':
				this.runs = event.runs;
				break;
			case 'calibration':
				this.calibration = event.calibration;
				break;
			case 'error':
				this.error = event.message;
				break;
		}
	}

	/**
	 * The browser reconnects an EventSource on its own schedule, which is neither
	 * observable nor bounded here; closing it and scheduling the retry ourselves
	 * is what makes `LINK LOST` and the backoff mean anything.
	 */
	#fail(): void {
		this.link = 'lost';
		this.#source?.close();
		this.#source = null;
		void this.#diagnose();
		const delay = nextDelay(this.#attempt);
		this.#attempt += 1;
		if (this.#retry) clearTimeout(this.#retry);
		this.#retry = setTimeout(() => {
			this.#retry = null;
			this.#open();
		}, delay);
	}

	/**
	 * An EventSource cannot read the body of a failed response, so a misconfigured
	 * server would otherwise be an unexplained blank screen. One plain request to
	 * a REST mirror recovers the 503's message.
	 */
	async #diagnose(): Promise<void> {
		try {
			const response = await fetch('/api/runs', { headers: { accept: 'application/json' } });
			if (response.ok) return;
			const body = (await response.json()) as { error?: string };
			this.error = body.error ?? `the monitor answered ${response.status}`;
		} catch {
			this.error = 'no answer from the monitor server';
		}
	}

	// -------------------------------------------------------------- navigation

	/** Pin a run: the URL carries it, so the view survives a reload and can be shared. */
	pin(runId: string): Promise<void> {
		return this.#navigate(runId);
	}

	/** Back to following whichever run is newest. */
	follow(): Promise<void> {
		return this.#navigate(null);
	}

	async #navigate(runId: string | null): Promise<void> {
		if (typeof window === 'undefined') return;
		const url = new URL(window.location.href);
		if (runId) url.searchParams.set('run', runId);
		else url.searchParams.delete('run');
		await goto(`${url.pathname}${url.search}`, {
			replaceState: true,
			keepFocus: true,
			noScroll: true
		});
	}

	// ----------------------------------------------------------------- drawers

	toggleDrawer(name: Exclude<DrawerName, null>): void {
		this.drawer = this.drawer === name ? null : name;
	}

	closeDrawer(): void {
		this.drawer = null;
	}

	setMotion(on: boolean): void {
		this.motion = on;
		writeStored(MOTION_KEY, on ? 'on' : 'off');
	}

	/** Open INSPECT on one trace file of the run currently on screen. */
	async openFile(path: string): Promise<void> {
		const runId = this.run?.header.runId;
		this.drawer = 'inspect';
		if (!runId) {
			this.inspect = {
				path,
				loading: false,
				error: 'no run on screen',
				bytes: 0,
				truncated: false,
				content: ''
			};
			return;
		}
		const seq = ++this.#inspectSeq;
		this.inspect = { path, loading: true, error: null, bytes: 0, truncated: false, content: '' };
		try {
			const response = await fetch(
				`/api/runs/${encodeURIComponent(runId)}/file?path=${encodeURIComponent(path)}`
			);
			const body = (await response.json()) as {
				error?: string;
				bytes?: number;
				truncated?: boolean;
				content?: string;
			};
			if (seq !== this.#inspectSeq) return; // a later click won
			if (!response.ok) {
				this.inspect = {
					path,
					loading: false,
					error: body.error ?? `read failed (${response.status})`,
					bytes: 0,
					truncated: false,
					content: ''
				};
				return;
			}
			this.inspect = {
				path,
				loading: false,
				error: null,
				bytes: body.bytes ?? 0,
				truncated: body.truncated ?? false,
				content: body.content ?? ''
			};
		} catch (err) {
			if (seq !== this.#inspectSeq) return;
			this.inspect = {
				path,
				loading: false,
				error: (err as Error).message,
				bytes: 0,
				truncated: false,
				content: ''
			};
		}
	}
}

/** One store per tab: the SSE budget is six connections per origin, and this is the one. */
export const monitor = new MonitorStore();
