import { readdir, readFile, stat } from 'node:fs/promises';
import { basename, dirname, join, resolve } from 'node:path';
import type { Colour, Face, RunRef, RunSummary } from '$lib/types';
import type {
	CandidateJson,
	ConversationMessage,
	JudgeCallJson,
	JudgeJson,
	RunFiles,
	RunJson,
	SelectJson,
	TaskJson,
	VerifyResultJson
} from './trace';
import { outcomeOfSelection } from './snapshot';

/** A run answer read for the lane preview is cut here; the drawer reads the file. */
const ANSWER_PREVIEW_BYTES = 4096;

async function isDir(path: string): Promise<boolean> {
	try {
		return (await stat(path)).isDirectory();
	} catch {
		return false;
	}
}

async function isFile(path: string): Promise<boolean> {
	try {
		return (await stat(path)).isFile();
	} catch {
		return false;
	}
}

async function mtimeMs(path: string): Promise<number | null> {
	try {
		return (await stat(path)).mtimeMs;
	} catch {
		return null;
	}
}

/** Parse a JSON file, treating an unreadable or half-written one as absent. */
async function readJson<T>(path: string): Promise<T | null> {
	try {
		return JSON.parse(await readFile(path, 'utf8')) as T;
	} catch {
		return null;
	}
}

async function readText(path: string): Promise<string | null> {
	try {
		return await readFile(path, 'utf8');
	} catch {
		return null;
	}
}

/**
 * The presentation seed as written, before JSON.parse rounds the int64 it is.
 * Null when the document does not carry one.
 */
export function rawPresentationSeed(judgeText: string): string | null {
	const match = /"presentation"\s*:\s*\{[^{}]*?"seed"\s*:\s*(-?\d+)/.exec(judgeText);
	return match ? match[1] : null;
}

// CMoA writes every JSON file through a dot-prefixed temporary name and renames
// it into place, so a name starting with `.` is a write in progress, never a run
// or a trace file.
const visible = (name: string) => !name.startsWith('.');

async function listDirs(path: string): Promise<string[]> {
	try {
		const entries = await readdir(path, { withFileTypes: true });
		return entries.filter((e) => e.isDirectory() && visible(e.name)).map((e) => e.name);
	} catch {
		return [];
	}
}

async function listFiles(path: string): Promise<string[]> {
	try {
		const entries = await readdir(path, { withFileTypes: true });
		return entries.filter((e) => e.isFile() && visible(e.name)).map((e) => e.name);
	} catch {
		return [];
	}
}

/** The directory that holds a run's `task.json` / `conversation.json`, if any. */
async function requestDirOf(taskDir: string): Promise<string | undefined> {
	return (await isFile(join(taskDir, 'conversation.json'))) ? taskDir : undefined;
}

async function runsUnderTask(taskDir: string, root: string): Promise<RunRef[]> {
	const requestDir = await requestDirOf(taskDir);
	const names = await listDirs(join(taskDir, 'runs'));
	const refs: RunRef[] = [];
	for (const name of names) {
		const dir = join(taskDir, 'runs', name);
		if (await isFile(join(dir, 'run.json'))) refs.push({ id: name, dir, requestDir, root });
	}
	return refs;
}

/**
 * Walk the monitored roots. A root is one of three things and the kind is read
 * off the directory rather than configured: a run directory (`run.json`), a task
 * or serve-request directory (`runs/`), or a serve root whose children are
 * request directories.
 *
 * Runs come back sorted by id descending, which is newest first: a run id starts
 * with a UTC timestamp, so lexicographic order is chronological order.
 */
export async function discoverRuns(roots: string[]): Promise<RunRef[]> {
	const found: RunRef[] = [];
	for (const raw of roots) {
		const root = resolve(raw);
		if (!(await isDir(root))) continue;

		if (await isFile(join(root, 'run.json'))) {
			// A run directory named directly. Its grandparent is the task dir when
			// the layout is <task>/runs/<run>.
			const parent = dirname(root);
			const requestDir =
				basename(parent) === 'runs' ? await requestDirOf(dirname(parent)) : undefined;
			found.push({ id: basename(root), dir: root, requestDir, root });
			continue;
		}

		if (await isDir(join(root, 'runs'))) {
			found.push(...(await runsUnderTask(root, root)));
			continue;
		}

		for (const child of await listDirs(root)) {
			const dir = join(root, child);
			if (await isDir(join(dir, 'runs'))) found.push(...(await runsUnderTask(dir, root)));
		}
	}
	found.sort((a, b) => (a.id < b.id ? 1 : a.id > b.id ? -1 : 0));
	return found;
}

/** The newest run of a list `discoverRuns` produced. */
export function latestRun(refs: RunRef[]): RunRef | null {
	return refs.length > 0 ? refs[0] : null;
}

// A run that has written select.json can never change again, so its files are
// cached for the life of the process and re-read for nobody.
const immutableCache = new Map<string, RunFiles>();

export function clearRunCache(): void {
	immutableCache.clear();
}

/** Read one run directory into memory. Missing or half-written files are absent. */
export async function readRunFiles(ref: RunRef): Promise<RunFiles> {
	const cached = immutableCache.get(ref.dir);
	if (cached) return cached;

	const rel = (...parts: string[]) => join(ref.dir, ...parts);
	const mtimes: Record<string, number> = {};
	const note = async (relPath: string) => {
		const t = await mtimeMs(join(ref.dir, relPath));
		if (t !== null) mtimes[relPath] = t;
	};

	const [run, judgeText, select] = await Promise.all([
		readJson<RunJson>(rel('run.json')),
		readText(rel('judge.json')),
		readJson<SelectJson>(rel('select.json'))
	]);
	let judge: JudgeJson | null = null;
	try {
		judge = judgeText === null ? null : (JSON.parse(judgeText) as JudgeJson);
	} catch {
		judge = null;
	}
	const judgeSeedRaw = judge && judgeText ? rawPresentationSeed(judgeText) : null;
	await Promise.all([note('run.json'), note('judge.json'), note('select.json')]);

	const candidates: Record<string, CandidateJson> = {};
	const answers: Record<string, string> = {};
	for (const name of await listFiles(rel('candidates'))) {
		if (name.endsWith('.raw.txt')) continue;
		if (name.endsWith('.json')) {
			const id = name.slice(0, -'.json'.length);
			const parsed = await readJson<CandidateJson>(rel('candidates', name));
			if (parsed) {
				candidates[id] = parsed;
				await note(join('candidates', name));
			}
		} else if (name.endsWith('.txt')) {
			const id = name.slice(0, -'.txt'.length);
			try {
				const body = await readFile(rel('candidates', name), 'utf8');
				answers[id] = body.slice(0, ANSWER_PREVIEW_BYTES);
			} catch {
				// an answer that cannot be read simply has no preview
			}
		}
	}

	const verify: Record<string, VerifyResultJson> = {};
	const verifyDirs = await listDirs(rel('verify'));
	for (const id of verifyDirs) {
		await note(join('verify', id));
		const parsed = await readJson<VerifyResultJson>(rel('verify', id, 'result.json'));
		if (parsed) verify[id] = parsed;
	}

	const judgeCalls: Record<string, JudgeCallJson> = {};
	for (const name of await listFiles(rel('judge'))) {
		if (!name.endsWith('.json')) continue;
		const parsed = await readJson<JudgeCallJson>(rel('judge', name));
		if (parsed) {
			judgeCalls[name.slice(0, -'.json'.length)] = parsed;
			await note(join('judge', name));
		}
	}

	let task: TaskJson | null = null;
	let conversation: ConversationMessage[] | null = null;
	if (ref.requestDir) {
		task = await readJson<TaskJson>(join(ref.requestDir, 'task.json'));
		const parsed = await readJson<ConversationMessage[]>(join(ref.requestDir, 'conversation.json'));
		if (Array.isArray(parsed)) conversation = parsed;
	}

	const files: RunFiles = {
		ref,
		run,
		task,
		conversation,
		candidates,
		answers,
		verify,
		judgeCalls,
		judge,
		judgeSeedRaw,
		select,
		verifyDirs,
		mtimes,
		complete: select !== null,
		readAtMs: Date.now()
	};
	if (files.complete) immutableCache.set(ref.dir, files);
	return files;
}

/** The last thing the person asked, which is what the proposers answered. */
export function lastUserMessage(conversation: ConversationMessage[] | null): string | null {
	if (!conversation) return null;
	for (let i = conversation.length - 1; i >= 0; i--) {
		const message = conversation[i];
		if (message?.role === 'user' && typeof message.content === 'string') return message.content;
	}
	return null;
}

function collapse(text: string): string {
	return text.replace(/\s+/g, ' ').trim();
}

/** One row of the run history: enough to choose a run, and nothing more. */
export function summariseRun(files: RunFiles): RunSummary {
	const face: Face = (files.run?.face as Face) ?? 'coding';
	const outcome = files.select
		? outcomeOfSelection(files.select)
		: { text: 'in progress', state: 'run', colour: 'run' as Colour };
	const preview =
		collapse(lastUserMessage(files.conversation) ?? '').slice(0, 80) ||
		files.run?.task?.id ||
		files.task?.id ||
		files.ref.id;
	return {
		id: files.run?.run_id ?? files.ref.id,
		dir: files.ref.dir,
		face,
		createdAt: files.run?.created_at ?? null,
		outcome: outcome.text,
		outcomeState: outcome.state,
		outcomeColour: outcome.colour,
		preview
	};
}
