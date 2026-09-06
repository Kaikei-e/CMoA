import { monitor } from './monitor.svelte';
import {
	faultOfBody,
	forwardableTranscript,
	runOfExtension,
	type CmoaExtension,
	type CommFault,
	type CommMessage
} from './comm';

export * from './comm';

const STORE_KEY = 'cmoa-monitor.comm';

/** One completion, as far as the panel reads it. */
interface CompletionBody {
	choices?: { message?: { content?: string } }[];
	cmoa?: CmoaExtension;
}

/**
 * The chat side of the monitor: the transcript, and the one request that asks
 * `cmoa serve` to run a round.
 *
 * The monitor still writes nothing. It relays the conversation and then does
 * what it always does — follow the trace the pool writes, and pin the run the
 * answer came from so the round that produced it stays on screen.
 *
 * The transcript is `$state.raw` and always replaced whole: a message is an
 * immutable record of a turn, and nothing edits one in place.
 */
export class CommStore {
	messages = $state.raw<CommMessage[]>([]);
	pending = $state(false);
	fault = $state<CommFault | null>(null);
	/** When the round in flight started, for the elapsed readout. */
	startedAtMs = $state(0);

	#restored = false;

	/**
	 * Read the transcript back out of session storage. Called from the panel
	 * rather than the constructor: the server renders an empty transcript, and
	 * filling it before hydration would make the two disagree.
	 */
	restore(): void {
		if (this.#restored) return;
		this.#restored = true;
		try {
			const raw = globalThis.sessionStorage?.getItem(STORE_KEY);
			if (!raw) return;
			const parsed = JSON.parse(raw);
			if (Array.isArray(parsed)) this.messages = parsed as CommMessage[];
		} catch {
			// a private window, or a transcript written by an older version
		}
	}

	#save(): void {
		try {
			globalThis.sessionStorage?.setItem(STORE_KEY, JSON.stringify(this.messages));
		} catch {
			// the transcript simply does not survive the tab
		}
	}

	/** Mark the newest user turn unanswered, so it is kept but never forwarded. */
	#markUnanswered(): void {
		const next = [...this.messages];
		for (let i = next.length - 1; i >= 0; i -= 1) {
			if (next[i].role === 'user') {
				next[i] = { ...next[i], unanswered: true };
				break;
			}
		}
		this.messages = next;
	}

	/**
	 * Send one turn. The whole answered transcript goes with it: `cmoa serve`
	 * has no session of its own, so multi-turn is the client's to carry.
	 */
	async send(text: string): Promise<void> {
		const content = text.trim();
		if (!content || this.pending) return;

		this.fault = null;
		this.messages = [...this.messages, { role: 'user', content, at: Date.now() }];
		const messages = forwardableTranscript(this.messages);
		this.pending = true;
		this.startedAtMs = Date.now();
		this.#save();
		// The round is about to start; the HUD should be watching for it.
		void monitor.follow();

		try {
			const response = await fetch('/api/chat', {
				method: 'POST',
				headers: { 'content-type': 'application/json', accept: 'application/json' },
				body: JSON.stringify({ messages })
			});
			const body: unknown = await response.json().catch(() => null);
			if (!response.ok) {
				const fault = faultOfBody(body, response.status);
				this.#markUnanswered();
				this.fault = fault;
				// Even a refused round wrote a trace; pinning it is how the person
				// sees why nobody was selected.
				if (fault.runId) void monitor.pin(fault.runId);
				return;
			}
			const completion = body as CompletionBody | null;
			const answer = completion?.choices?.[0]?.message?.content ?? '';
			const run = runOfExtension(completion?.cmoa) ?? undefined;
			this.messages = [...this.messages, { role: 'assistant', content: answer, at: Date.now(), run }];
			if (run) void monitor.pin(run.id);
		} catch (err) {
			this.#markUnanswered();
			this.fault = { type: 'monitor', code: '', message: (err as Error).message };
		} finally {
			this.pending = false;
			this.#save();
		}
	}

	clear(): void {
		this.messages = [];
		this.fault = null;
		try {
			globalThis.sessionStorage?.removeItem(STORE_KEY);
		} catch {
			// nothing to do
		}
	}
}

/** One transcript per tab, like the one stream it shares the page with. */
export const comm = new CommStore();
