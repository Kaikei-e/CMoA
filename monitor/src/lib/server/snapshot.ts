import type {
	Colour,
	Face,
	FleetSample,
	JudgeCell,
	JudgeCellState,
	JudgePair,
	JudgePanel,
	JudgeVerdict,
	Lane,
	LaneState,
	MonitorConfig,
	RunPhase,
	RunSnapshot,
	SelectPanel,
	TimelineEvent
} from '$lib/types';
import { JUDGE_ID } from './fleet';
import type { CandidateJson, JudgeCallJson, JudgeOrderJson, RunFiles, SelectJson } from './trace';

/** The width of the answer excerpt watch.sh shows on a chat lane. */
const ANSWER_COLUMNS = 60;

/**
 * watch.sh's `scolor()`: every state word in the display belongs to one of four
 * colour categories, and the word list is the contract the UI paints against.
 */
export function colourOf(state: string): Colour {
	switch (state) {
		case 'ok':
		case 'pass':
		case 'done':
		case 'selected':
			return 'ok';
		case 'run':
		case 'running':
		case 'generating':
		case 'prefill':
		case 'busy':
			return 'run';
		case 'bad':
		case 'failed':
		case 'invalid':
		case 'timeout':
		case 'error':
		case 'unreachable':
		case 'no_candidate':
			return 'bad';
		default:
			return 'dim';
	}
}

/** Collapse whitespace and cut to `n` characters. */
export function cut(value: unknown, n: number): string {
	return String(value ?? '')
		.replace(/\s+/g, ' ')
		.trim()
		.slice(0, n);
}

/** Cut to a display width, a non-ASCII character counting as two columns. */
export function widthCut(value: unknown, columns: number): string {
	const text = String(value ?? '')
		.replace(/\s+/g, ' ')
		.trim();
	let width = 0;
	let out = '';
	for (const ch of text) {
		width += ch.codePointAt(0)! < 128 ? 1 : 2;
		if (width > columns) break;
		out += ch;
	}
	return out;
}

/**
 * Milliseconds from an RFC 3339 timestamp. CMoA writes nanoseconds, which
 * `Date.parse` does not promise to accept, so the fraction is cut to millis.
 */
export function timestampMs(value: unknown): number | null {
	if (typeof value !== 'string' || value.trim() === '') return null;
	const trimmed = value.trim().replace(/\.(\d{3})\d+/, '.$1');
	const ms = Date.parse(trimmed);
	return Number.isFinite(ms) ? ms : null;
}

function seconds(ms: unknown): string {
	return typeof ms === 'number' && Number.isFinite(ms) ? `${(ms / 1000).toFixed(1)}s` : '-';
}

function round1(value: number): number {
	return Math.round(value * 10) / 10;
}

// ------------------------------------------------------------------ lanes

interface LaneSpec {
	id: string;
	model: string;
	maxTokens: number;
	baseUrl: string;
}

/**
 * The lanes to display. A run records the proposers it actually used, so a
 * historical run keeps its own models and token budgets rather than borrowing
 * whatever cmoa.json says today; the live config only fills the gaps.
 */
function laneSpecs(files: RunFiles | null, config: MonitorConfig): LaneSpec[] {
	const byId = new Map(config.proposers.map((p) => [p.id, p]));
	const fromRun = files?.run?.proposers ?? [];
	const budgets = new Map(
		(files?.run?.config?.proposers ?? [])
			.filter((p) => typeof p.id === 'string')
			.map((p) => [p.id as string, p.max_tokens])
	);
	const specs = fromRun.length > 0 ? fromRun : config.proposers.map((p) => ({ id: p.id, model: p.model }));
	return specs
		.filter((p): p is { id: string; model?: string } => typeof p.id === 'string' && p.id !== '')
		.map((p) => {
			const configured = byId.get(p.id);
			return {
				id: p.id,
				model: p.model || configured?.model || '?',
				maxTokens: budgets.get(p.id) || configured?.maxTokens || 1024,
				baseUrl: configured?.baseUrl ?? ''
			};
		});
}

function candidateInfo(face: Face, candidate: CandidateJson, answer: string): string {
	if (face === 'chat') {
		const text = candidate.status === 'ok' ? answer : '';
		return text.trim() ? widthCut(text, ANSWER_COLUMNS) : cut(candidate.error || '-', ANSWER_COLUMNS);
	}
	const diff = candidate.diff;
	if (diff) {
		const files = diff.files?.length ?? 0;
		return `${files} file(s) +${diff.additions ?? 0}/-${diff.deletions ?? 0}`;
	}
	return cut(candidate.error || '-', 34);
}

function deriveLanes(
	files: RunFiles | null,
	face: Face,
	fleet: FleetSample[],
	config: MonitorConfig
): Lane[] {
	const samples = new Map(fleet.filter((s) => s.role === 'proposer').map((s) => [s.id, s]));
	return laneSpecs(files, config).map((spec) => {
		const sample = samples.get(spec.id) ?? null;
		const candidate = files?.candidates[spec.id] ?? null;
		const completionTokens = candidate?.usage?.completion_tokens ?? 0;
		// Once the candidate file exists it is authoritative: the slot has been
		// released and its decoded counter has already been reset by the server.
		const decoded = completionTokens || sample?.decoded || 0;

		let state: LaneState = 'idle';
		if (candidate) state = candidate.status === 'ok' ? 'done' : 'failed';
		else if (sample && !sample.reachable) state = 'unreachable';
		else if (sample && sample.processing > 0) state = decoded > 0 ? 'generating' : 'prefill';

		let note = '';
		if (!candidate) {
			if (sample && !sample.reachable) note = `server unreachable (${spec.baseUrl || sample.baseUrl})`;
			else if (sample && sample.promptTokens > 0 && sample.processing > 0 && decoded === 0)
				note = `prefill ${sample.promptProcessed}/${sample.promptTokens} prompt tok`;
			else note = 'waiting for candidate...';
		}

		const verifyStatus = files?.verify[spec.id]?.status ?? '-';
		return {
			id: spec.id,
			model: spec.model,
			state,
			colour: colourOf(state),
			barFraction: Math.min(1, Math.max(0, decoded / (spec.maxTokens > 0 ? spec.maxTokens : 1024))),
			decoded,
			tokPerSec: sample?.tokPerSec ?? 0,
			maxTokens: spec.maxTokens,
			promptTokens: sample?.promptTokens ?? 0,
			promptProcessed: sample?.promptProcessed ?? 0,
			reachable: sample?.reachable ?? false,
			live: sample !== null,
			note,
			candidate: candidate
				? {
						status: candidate.status ?? '?',
						info: candidateInfo(face, candidate, files?.answers[spec.id] ?? ''),
						timing: `${((candidate.timings?.request_ms ?? 0) / 1000).toFixed(1)}s ${(
							candidate.timings?.tokens_per_second ?? 0
						).toFixed(1)}tok/s`,
						completionTokens,
						reasoningBytes: candidate.reasoning_bytes ?? 0,
						colour: candidate.status === 'ok' ? 'ok' : 'bad'
					}
				: null,
			// The verify column has its own three-way colouring in watch.sh: a
			// verifier answers pass, or no, and `fail` is not one of scolor's words.
			verify:
				face === 'chat'
					? null
					: {
							status: verifyStatus,
							colour: verifyStatus === 'pass' ? 'ok' : verifyStatus === '-' ? 'dim' : 'bad'
						}
		};
	});
}

// ------------------------------------------------------------------ judge grid

function cell(state: JudgeCellState, text: string, who: string | null): JudgeCell {
	return { state, text, who, colour: colourOf(state) };
}

/**
 * One order of one pair. `record` is judge.json's summary when it exists; the
 * call file stands in before judge.json is written, which is what makes the
 * grid fill in live rather than all at once at the end.
 */
function orderCell(
	tag: 'ab' | 'ba',
	record: JudgeOrderJson | JudgeCallJson | null,
	judging: boolean
): JudgeCell {
	if (!record) {
		return judging ? cell('run', `${tag} running`, null) : cell('pend', `${tag} pending`, null);
	}
	const status = record.status || '?';
	const secs = seconds(record.latency_ms);
	if (status === 'ok') {
		const choice = record.choice || '?';
		let who = record.choice_candidate ?? null;
		if (!who && (choice === 'A' || choice === 'B')) {
			who = (choice === 'A' ? record.first : record.second) ?? null;
		}
		if (choice === 'tie') return cell('ok', `${tag} ok tie ${secs}`, 'tie');
		return cell('ok', `${tag} ok ${choice}>${who || '?'} ${secs}`, who);
	}
	if (status === 'invalid_output') return cell('invalid', `${tag} invalid ${secs}`, null);
	if (status === 'timeout') return cell('timeout', `${tag} timeout ${secs}`, null);
	return cell('error', `${tag} ${cut(status, 8)} ${secs}`, null);
}

function verdictOf(
	pairRecorded: { verdict?: string; draw_reason?: string } | null,
	ab: JudgeCell,
	ba: JudgeCell,
	judging: boolean
): JudgeVerdict {
	if (pairRecorded) {
		if (pairRecorded.verdict === 'draw') {
			return {
				state: 'bad',
				text: `draw (${pairRecorded.draw_reason || '?'})`,
				colour: 'bad',
				provisional: false
			};
		}
		return {
			state: 'ok',
			text: `-> ${pairRecorded.verdict ?? '?'}`,
			colour: 'ok',
			provisional: false
		};
	}
	// judge.json is written once, at the end. Until then both call files together
	// already decide the pair, so the grid shows the verdict marked provisional.
	if (ab.who && ba.who) {
		if (ab.who === 'tie' || ba.who === 'tie')
			return { state: 'bad', text: '~draw (tie)', colour: 'bad', provisional: true };
		if (ab.who === ba.who)
			return { state: 'ok', text: `~${ab.who}`, colour: 'ok', provisional: true };
		return { state: 'bad', text: '~draw (disagree)', colour: 'bad', provisional: true };
	}
	return judging
		? { state: 'run', text: 'judging', colour: 'run', provisional: false }
		: { state: 'pend', text: '-', colour: 'dim', provisional: false };
}

function judgeOutcome(files: RunFiles | null): JudgePanel['outcome'] {
	const outcome = files?.judge?.outcome;
	if (!outcome) return { state: 'pend', text: '-', colour: 'dim' };
	const kind = outcome.kind ?? '?';
	if (kind === 'selected') {
		return {
			state: 'selected',
			text: `selected ${outcome.candidate_id ?? '?'}  ${cut(outcome.reason ?? '', 46)}`.trimEnd(),
			colour: 'ok'
		};
	}
	if (kind === 'no_candidate') {
		return {
			state: 'no_candidate',
			text: `no_candidate (${outcome.reason || '?'})`,
			colour: 'bad'
		};
	}
	if (kind === 'judge_timeout') {
		return {
			state: 'timeout',
			text: `judge_timeout after ${outcome.after_ms ?? '?'}ms`,
			colour: 'bad'
		};
	}
	if (kind === 'judge_failed') {
		return { state: 'error', text: `judge_failed: ${cut(outcome.error ?? '', 40)}`, colour: 'bad' };
	}
	return { state: 'bad', text: kind, colour: 'bad' };
}

function deriveJudge(
	files: RunFiles | null,
	laneIds: string[],
	fleet: FleetSample[],
	config: MonitorConfig
): JudgePanel {
	const sample = fleet.find((s) => s.id === JUDGE_ID) ?? null;
	const judge = files?.judge ?? null;
	const select = files?.select ?? null;

	const callsSeen = Object.keys(files?.judgeCalls ?? {}).length > 0;
	const allProposed = laneIds.length > 0 && laneIds.every((id) => !!files?.candidates[id]);
	let candidates = judge?.candidates ?? [];
	if (candidates.length === 0 && allProposed) {
		// Before propose finishes any proposer may still make the grid, so the
		// pairs are only narrowed to the ok candidates once every answer is in.
		candidates = laneIds.filter((id) => files?.candidates[id]?.status === 'ok');
	}
	if (candidates.length < 2) candidates = laneIds;
	const judging = !!judge || callsSeen || (allProposed && !select);

	const recorded = judge?.pairs ?? [];
	// judge.json with no pairs is a round the candidates settled among
	// themselves: the judge was never asked, so there is no grid to draw.
	// Without this the finished round would show three cells running for ever.
	const askedTheJudge = !judge || recorded.length > 0;
	const pairs: JudgePair[] = [];
	let index = 0;
	for (let i = 0; askedTheJudge && i < candidates.length; i++) {
		for (let k = i + 1; k < candidates.length; k++) {
			const pairRecorded = index < recorded.length ? recorded[index] : null;
			const a = pairRecorded?.pair?.[0] ?? candidates[i];
			const b = pairRecorded?.pair?.[1] ?? candidates[k];
			const orders = pairRecorded?.orders ?? [];
			const ab = orderCell('ab', orders[0] ?? files?.judgeCalls[`${index}-ab`] ?? null, judging);
			const ba = orderCell('ba', orders[1] ?? files?.judgeCalls[`${index}-ba`] ?? null, judging);
			pairs.push({
				index,
				a,
				b,
				label: cut(`${a}|${b}`, 17),
				ab,
				ba,
				verdict: verdictOf(pairRecorded, ab, ba, judging)
			});
			index++;
		}
	}

	const presentation = judge?.presentation ?? null;
	// The seed is an int64; only the text judge.json holds is exact.
	const seed = files?.judgeSeedRaw ?? (presentation ? String(presentation.seed ?? '-') : '-');
	const wins = judge?.wins ?? {};
	const configured = files?.run?.config?.judge ?? null;

	return {
		model: configured?.model || config.judge?.model || '?',
		baseUrl: config.judge?.baseUrl ?? '',
		parallel: configured?.parallel ?? config.judge?.parallel ?? 0,
		server: {
			reachable: sample?.reachable ?? false,
			slotCount: sample?.slotCount ?? 0,
			busy: sample?.processing ?? 0,
			decoded: sample?.decoded ?? 0,
			tokPerSec: sample?.tokPerSec ?? 0,
			slots: sample?.slots ?? []
		},
		presentation: presentation
			? {
					seed,
					source: String(presentation.seed_source ?? '-'),
					nonce: String(presentation.nonce ?? '-')
				}
			: null,
		seedText: presentation
			? `seed ${seed} (${presentation.seed_source ?? '-'})  nonce ${presentation.nonce ?? '-'}`
			: 'seed -  nonce - (no judge.json yet)',
		candidates,
		pairs,
		wins: candidates.map((id) => ({ id, wins: wins[id] ?? 0 })),
		swapConsistent:
			judge && typeof judge.swap_consistent_pairs === 'number'
				? { pairs: judge.swap_consistent_pairs, total: recorded.length }
				: null,
		retries: judge?.invalid_output_retries ?? 0,
		outcome: judgeOutcome(files),
		latencyMs: typeof judge?.latency_ms === 'number' ? judge.latency_ms : null,
		latencyText: seconds(judge?.latency_ms)
	};
}

// ------------------------------------------------------------------ select

/** The select line watch.sh prints, without the ranked and also_passed tails. */
export function outcomeOfSelection(select: SelectJson): {
	text: string;
	state: string;
	colour: Colour;
} {
	const selection = select.selection ?? {};
	const kind = selection.kind ?? '?';
	if (kind === 'selected')
		return { text: `selected ${selection.candidate_id ?? '?'}`, state: 'selected', colour: 'ok' };
	if (kind === 'no_candidate') {
		const tried = Array.isArray(selection.tried) ? selection.tried.join(',') : String(selection.tried ?? '');
		const reason = selection.reason ? selection.reason : `tried ${tried}`;
		return { text: `no_candidate (${reason})`, state: 'no_candidate', colour: 'bad' };
	}
	if (kind === 'verifier_failed')
		return {
			text: `verifier_failed: ${cut(selection.error ?? '', 40)}`,
			state: 'error',
			colour: 'bad'
		};
	if (kind === 'judge_timeout')
		return {
			text: `judge_timeout after ${selection.after_ms ?? '?'}ms`,
			state: 'timeout',
			colour: 'bad'
		};
	if (kind === 'judge_failed')
		return { text: `judge_failed: ${cut(selection.error ?? '', 40)}`, state: 'error', colour: 'bad' };
	return { text: kind, state: 'bad', colour: 'bad' };
}

function deriveSelect(files: RunFiles | null): SelectPanel {
	if (!files) return { state: 'pend', colour: 'dim', text: '-', kind: null, ranked: [], alsoPassed: [] };
	if (!files.select) {
		return {
			state: 'pend',
			colour: 'dim',
			text: 'waiting for select.json...',
			kind: null,
			ranked: [],
			alsoPassed: []
		};
	}
	const outcome = outcomeOfSelection(files.select);
	return {
		state: outcome.state,
		colour: outcome.colour,
		text: outcome.text,
		kind: files.select.selection?.kind ?? null,
		ranked: files.select.ranked ?? [],
		alsoPassed: files.select.also_passed ?? []
	};
}

// ------------------------------------------------------------------ timeline

function deriveTimeline(files: RunFiles | null, face: Face, laneIds: string[]): TimelineEvent[] {
	const t0 = timestampMs(files?.run?.created_at);
	if (!files || t0 === null) return [];
	const events: TimelineEvent[] = [{ offsetSec: 0, label: 'propose' }];

	for (const id of laneIds) {
		const candidate = files.candidates[id];
		if (!candidate) continue;
		const at = timestampMs(candidate.finished_at) ?? files.mtimes[`candidates/${id}.json`] ?? null;
		if (at !== null) events.push({ offsetSec: round1((at - t0) / 1000), label: id });
	}

	if (face === 'chat') {
		let start: number | null = null;
		const judge = files.judge;
		if (judge && typeof judge.latency_ms === 'number') {
			const finished = timestampMs(judge.finished_at);
			if (finished !== null) start = finished - judge.latency_ms;
		}
		if (start === null) {
			// No judge.json yet: the earliest call file, backed off by its own
			// latency, is the closest thing to when the judge was first asked.
			const starts: number[] = [];
			for (const [name, call] of Object.entries(files.judgeCalls)) {
				const at = files.mtimes[`judge/${name}.json`];
				if (at === undefined) continue;
				starts.push(typeof call.latency_ms === 'number' ? at - call.latency_ms : at);
			}
			if (starts.length > 0) start = Math.min(...starts);
		}
		if (start !== null) events.push({ offsetSec: round1((start - t0) / 1000), label: 'judge' });
	} else {
		const starts: number[] = [];
		for (const id of files.verifyDirs) {
			const at = timestampMs(files.verify[id]?.started_at) ?? files.mtimes[`verify/${id}`] ?? null;
			if (at !== null) starts.push(at);
		}
		if (starts.length > 0)
			events.push({ offsetSec: round1((Math.min(...starts) - t0) / 1000), label: 'verify' });
	}

	if (files.select) {
		const at = timestampMs(files.select.finished_at) ?? files.mtimes['select.json'] ?? null;
		if (at !== null) events.push({ offsetSec: round1((at - t0) / 1000), label: 'done' });
	}

	events.sort((a, b) => a.offsetSec - b.offsetSec);
	return events;
}

function derivePhase(files: RunFiles | null, face: Face, laneIds: string[]): RunPhase {
	if (!files || !files.run) return 'waiting';
	if (files.select) return 'done';
	const allProposed = laneIds.length > 0 && laneIds.every((id) => !!files.candidates[id]);
	if (face === 'chat') {
		if (files.judge || Object.keys(files.judgeCalls).length > 0 || allProposed) return 'judging';
	} else if (files.verifyDirs.length > 0) {
		return 'verifying';
	}
	return 'proposing';
}

// ------------------------------------------------------------------ snapshot

/**
 * Everything one screen shows about one run, derived from files already read
 * and a fleet sample already taken. Pure: no clock, no filesystem, no network,
 * so a fixture directory and a live run produce the same object.
 */
export function deriveSnapshot(
	files: RunFiles | null,
	fleet: FleetSample[],
	config: MonitorConfig,
	nowMs: number,
	mode: 'follow' | 'pinned' = 'follow'
): RunSnapshot {
	const face: Face = (files?.run?.face as Face) ?? (files?.task?.face as Face) ?? 'coding';
	const lanes = deriveLanes(files, face, fleet, config);
	const laneIds = lanes.map((lane) => lane.id);

	const createdAt = files?.run?.created_at ?? null;
	const t0 = timestampMs(createdAt);
	const finished = timestampMs(files?.select?.finished_at);
	const elapsedSec = t0 === null ? 0 : round1((((finished ?? nowMs) - t0) / 1000));

	return {
		header: {
			runId: files?.run?.run_id ?? files?.ref.id ?? null,
			dir: files?.ref.dir ?? null,
			requestDir: files?.ref.requestDir ?? null,
			face,
			createdAt,
			elapsedSec,
			phase: derivePhase(files, face, laneIds),
			mode,
			complete: files?.complete ?? false
		},
		lanes,
		judge: face === 'chat' && files ? deriveJudge(files, laneIds, fleet, config) : null,
		select: deriveSelect(files),
		timeline: deriveTimeline(files, face, laneIds)
	};
}
