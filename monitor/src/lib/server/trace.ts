import type { Face, RunRef } from '$lib/types';

// The trace shapes the monitor actually reads. Every field is optional: the
// monitor may see a directory mid-write, and a missing field must degrade the
// display rather than throw.

export interface RunTask {
	id?: string;
	dir?: string;
	repo?: string;
	files?: string[];
}

export interface RunProposer {
	id?: string;
	model?: string;
	base_url?: string;
}

export interface RunJson {
	schema_version?: number;
	run_id?: string;
	created_at?: string;
	face?: Face;
	task?: RunTask;
	proposers?: RunProposer[];
	candidates_origin?: string;
	config?: {
		proposers?: { id?: string; model?: string; max_tokens?: number; base_url?: string }[];
		judge?: { model?: string; base_url?: string; parallel?: number };
	};
}

export interface CandidateJson {
	proposer_id?: string;
	model?: string;
	status?: string;
	error?: string;
	finish_reason?: string;
	usage?: { prompt_tokens?: number; completion_tokens?: number; reasoning_tokens?: number };
	reasoning_bytes?: number;
	timings?: { request_ms?: number; tokens_per_second?: number };
	diff?: { files?: string[]; additions?: number; deletions?: number };
	answer_bytes?: number;
	started_at?: string;
	finished_at?: string;
}

export interface VerifyResultJson {
	candidate_id?: string;
	status?: string;
	exit_code?: number;
	duration_ms?: number;
	started_at?: string;
	finished_at?: string;
}

export interface JudgeOrderJson {
	first?: string;
	second?: string;
	choice?: string;
	choice_candidate?: string;
	status?: string;
	retries?: number;
	latency_ms?: number;
	file?: string;
}

export interface JudgePairJson {
	pair?: [string, string];
	orders?: JudgeOrderJson[];
	verdict?: string;
	draw_reason?: string;
}

export interface JudgeJson {
	run_id?: string;
	judge?: { model?: string; base_url?: string; parallel?: number };
	candidates?: string[];
	presentation?: { seed?: number | string; seed_source?: string; nonce?: string };
	pairs?: JudgePairJson[];
	wins?: Record<string, number>;
	outcome?: { kind?: string; candidate_id?: string; reason?: string; after_ms?: number; error?: string };
	ranked?: string[];
	swap_consistent_pairs?: number;
	invalid_output_retries?: number;
	latency_ms?: number;
	finished_at?: string;
}

/** One `judge/<pair>-<ab|ba>.json`, of which only the summary tail is displayed. */
export interface JudgeCallJson {
	pair?: number;
	order?: string;
	first?: string;
	second?: string;
	status?: string;
	choice?: string;
	choice_candidate?: string;
	latency_ms?: number;
}

export interface SelectJson {
	rule?: string;
	order?: string[];
	selection?: {
		kind?: string;
		candidate_id?: string;
		reason?: string;
		tried?: string[];
		error?: string;
		after_ms?: number;
	};
	also_passed?: string[];
	ranked?: string[];
	finished_at?: string;
}

export interface ConversationMessage {
	role?: string;
	content?: string;
}

export interface TaskJson {
	id?: string;
	face?: Face;
	version?: number;
	conversation?: string;
}

/**
 * Everything the monitor read out of one run directory in a single pass.
 * `deriveSnapshot` is a pure function of this object plus a fleet sample, so a
 * fixture directory and a live run go down exactly the same code path.
 */
export interface RunFiles {
	ref: RunRef;
	run: RunJson | null;
	task: TaskJson | null;
	conversation: ConversationMessage[] | null;
	/** By proposer id, for every `candidates/<id>.json` that parsed. */
	candidates: Record<string, CandidateJson>;
	/** By proposer id: the answer text of `candidates/<id>.txt` (chat face). */
	answers: Record<string, string>;
	/** By proposer id, for every `verify/<id>/result.json` that parsed. */
	verify: Record<string, VerifyResultJson>;
	/** By call name, e.g. `0-ab`. */
	judgeCalls: Record<string, JudgeCallJson>;
	judge: JudgeJson | null;
	/**
	 * `presentation.seed` exactly as judge.json spells it. The seed is an int64
	 * derived from the run id, which JSON.parse rounds to the nearest double, and
	 * a rounded seed no longer reproduces the selection it names.
	 */
	judgeSeedRaw: string | null;
	select: SelectJson | null;
	/** Proposer ids that have a `verify/<id>/` directory, result.json or not. */
	verifyDirs: string[];
	/** Relative path to modification time in milliseconds, for the timeline. */
	mtimes: Record<string, number>;
	/** True once select.json exists: the run is then immutable and cacheable. */
	complete: boolean;
	readAtMs: number;
}
