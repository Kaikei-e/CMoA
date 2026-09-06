// Shared vocabulary between the server modules and the browser.
// Nothing here may import from `node:*`: this file is bundled into the client.

/** The two CMoA faces. A run written before the field existed is a coding run. */
export type Face = 'chat' | 'coding';

/**
 * The four colour categories watch.sh's `scolor()` sorts every state word into.
 * The UI maps them to phosphor green / amber / red / dimmed.
 */
export type Colour = 'ok' | 'run' | 'bad' | 'dim';

/** Proposer lane states, in watch.sh's exact vocabulary. */
export type LaneState = 'idle' | 'unreachable' | 'prefill' | 'generating' | 'done' | 'failed';

// ---------------------------------------------------------------- configuration

export interface ProposerConfig {
	id: string;
	model: string;
	/** Base URL with any trailing `/v1` removed, so `/slots` can be appended. */
	baseUrl: string;
	maxTokens: number;
}

export interface JudgeConfig {
	baseUrl: string;
	model: string;
	parallel: number;
}

export interface ServeConfig {
	listen: string;
	/** Absolute: `runs_dir` is resolved against the directory holding cmoa.json. */
	runsDir: string;
}

export interface MonitorConfig {
	/** Absolute path of the cmoa.json this was read from. */
	path: string;
	proposers: ProposerConfig[];
	judge?: JudgeConfig;
	serve?: ServeConfig;
	/** `harness.vault`, used to default the calibrations directory. */
	vault?: string;
}

// ---------------------------------------------------------------- fleet health

export type ProbeMode = 'slots' | 'metrics';

/** What one `/slots` or `/metrics` document says, before any smoothing. */
export interface ServerReading {
	reachable: boolean;
	/** Busy slots on `/slots`; 1 or 0 from `llamacpp:requests_processing`. */
	processing: number;
	slotCount: number;
	promptTokens: number;
	promptProcessed: number;
	/**
	 * `/slots`: the tokens decoded so far by the requests in flight.
	 * `/metrics`: the server's lifetime `tokens_predicted_total` counter, which
	 * only becomes a per-run number after a baseline is subtracted — see `cumulative`.
	 */
	decoded: number;
	/** Per-slot decoded counts, in slot order. Empty in `/metrics` mode. */
	slots: number[];
	/** True when `decoded` is a lifetime counter rather than a per-request count. */
	cumulative: boolean;
}

/** One server as the fleet sampler last saw it. */
export interface FleetSample extends ServerReading {
	/** Proposer id, or `__judge__` for the judge server. */
	id: string;
	role: 'proposer' | 'judge';
	baseUrl: string;
	model: string;
	mode: ProbeMode;
	/** Smoothed decode rate; see `Fleet` for the smoothing rule. */
	tokPerSec: number;
}

export interface ServeHealth {
	listen: string;
	reachable: boolean;
}

export interface FleetState {
	sampledAtMs: number;
	servers: FleetSample[];
	serve: ServeHealth | null;
}

// ---------------------------------------------------------------- runs

/** One run directory, and where it was found. */
export interface RunRef {
	/** The run id, which is the run directory's name. */
	id: string;
	/** Absolute path of the run directory. */
	dir: string;
	/** Absolute path of the task or serve-request directory holding `runs/`, when there is one. */
	requestDir?: string;
	/** The monitored root this run was discovered under. */
	root: string;
}

/** A row of the run history list. */
export interface RunSummary {
	id: string;
	dir: string;
	face: Face;
	createdAt: string | null;
	/** Human-readable outcome, e.g. `selected gemma` or `no_candidate (no_majority)`. */
	outcome: string;
	outcomeState: string;
	outcomeColour: Colour;
	/** First 80 characters of the last user message, or the task id. */
	preview: string;
}

// ---------------------------------------------------------------- snapshot

export interface LaneCandidate {
	status: string;
	/** chat: the first characters of the answer; coding: `N file(s) +a/-d`. */
	info: string;
	/** `12.3s 8.1tok/s`, as watch.sh formats it. */
	timing: string;
	completionTokens: number;
	reasoningBytes: number;
	colour: Colour;
}

export interface LaneVerify {
	status: string;
	colour: Colour;
}

export interface Lane {
	id: string;
	model: string;
	state: LaneState;
	colour: Colour;
	/** `decoded / max_tokens`, clamped to 0..1. */
	barFraction: number;
	decoded: number;
	tokPerSec: number;
	maxTokens: number;
	promptTokens: number;
	promptProcessed: number;
	/** False when the fleet sampler could not reach this server. */
	reachable: boolean;
	/** True when the fleet has no sample for this lane at all (a historical run). */
	live: boolean;
	/** The second line watch.sh prints under a lane that has no candidate yet. */
	note: string;
	candidate: LaneCandidate | null;
	verify: LaneVerify | null;
}

export type JudgeCellState = 'ok' | 'invalid' | 'timeout' | 'error' | 'run' | 'pend';

export interface JudgeCell {
	state: JudgeCellState;
	/** `ab ok A>granite 19.1s`, as watch.sh formats it. */
	text: string;
	/** The candidate this order named, `tie`, or null when it named nobody. */
	who: string | null;
	colour: Colour;
}

export interface JudgeVerdict {
	state: 'ok' | 'bad' | 'run' | 'pend';
	text: string;
	colour: Colour;
	/**
	 * True when the verdict was inferred from the two call files because
	 * judge.json is not written yet. Provisional text is prefixed with `~`.
	 */
	provisional: boolean;
}

export interface JudgePair {
	index: number;
	a: string;
	b: string;
	/** `a|b`, truncated to 17 characters as watch.sh does. */
	label: string;
	ab: JudgeCell;
	ba: JudgeCell;
	verdict: JudgeVerdict;
}

export interface JudgeServer {
	reachable: boolean;
	slotCount: number;
	busy: number;
	decoded: number;
	tokPerSec: number;
	slots: number[];
}

export interface JudgePresentation {
	seed: string;
	source: string;
	nonce: string;
}

export interface JudgeOutcome {
	state: string;
	text: string;
	colour: Colour;
}

export interface JudgePanel {
	model: string;
	baseUrl: string;
	parallel: number;
	server: JudgeServer;
	presentation: JudgePresentation | null;
	/** `seed 7 (flag)  nonce 7f3a91c4`, or the placeholder before judge.json exists. */
	seedText: string;
	candidates: string[];
	pairs: JudgePair[];
	wins: { id: string; wins: number }[];
	swapConsistent: { pairs: number; total: number } | null;
	retries: number;
	outcome: JudgeOutcome;
	latencyMs: number | null;
	latencyText: string;
}

export interface SelectPanel {
	state: string;
	colour: Colour;
	/** The selection sentence only; `ranked` and `alsoPassed` are separate. */
	text: string;
	kind: string | null;
	ranked: string[];
	alsoPassed: string[];
}

export interface TimelineEvent {
	offsetSec: number;
	label: string;
}

/** Which part of the round the run is in, derived from the files alone. */
export type RunPhase = 'waiting' | 'proposing' | 'judging' | 'verifying' | 'done';

export interface SnapshotHeader {
	runId: string | null;
	dir: string | null;
	requestDir: string | null;
	face: Face;
	createdAt: string | null;
	/** Seconds from `created_at` to `select.finished_at`, or to now while running. */
	elapsedSec: number;
	phase: RunPhase;
	/** Whether the viewer pinned this run or is following the newest one. */
	mode: 'follow' | 'pinned';
	/** True once select.json exists, which makes the run immutable. */
	complete: boolean;
}

export interface RunSnapshot {
	header: SnapshotHeader;
	lanes: Lane[];
	/** Chat face only; null on the coding face and when there is no run. */
	judge: JudgePanel | null;
	select: SelectPanel;
	timeline: TimelineEvent[];
}

// ---------------------------------------------------------------- calibration

export interface Calibration {
	file: string;
	id: string;
	title: string;
	judge: string;
	verdict: string;
	date: string | null;
	pool: string | null;
	windowFrom: string | null;
	windowTo: string | null;
	inForceUntil: string | null;
	tieHandling: string | null;
	humanKappa: number | null;
	nHuman: number | null;
	swapKappa: number | null;
	rerunKappa: number | null;
	report: string | null;
	/** Computed against today's date, not stored in the document. */
	inForce: boolean;
}

export interface CalibrationState {
	dir: string | null;
	/** The newest calibration per judge. */
	entries: Calibration[];
}

// ---------------------------------------------------------------- trace files

export interface TraceFile {
	relPath: string;
	bytes: number;
	truncated: boolean;
	content: string;
}

export type TraceFileResult =
	| { ok: true; file: TraceFile }
	| { ok: false; reason: 'rejected' | 'missing' };
