import { readFileSync } from 'node:fs';
import { dirname, isAbsolute, resolve } from 'node:path';
import type { MonitorConfig, ProposerConfig } from '$lib/types';

/** The default polling period, in milliseconds. */
export const DEFAULT_INTERVAL_MS = 500;

export class ConfigError extends Error {}

/**
 * Drop a trailing `/v1` so `/slots` and `/metrics` can be appended.
 * This is watch.sh's `base()`.
 */
export function serverBase(url: string): string {
	const trimmed = (url ?? '').replace(/\/+$/, '');
	return trimmed.endsWith('/v1') ? trimmed.slice(0, -3).replace(/\/+$/, '') : trimmed;
}

function asRecord(value: unknown): Record<string, unknown> {
	return value && typeof value === 'object' && !Array.isArray(value)
		? (value as Record<string, unknown>)
		: {};
}

function asString(value: unknown): string {
	return typeof value === 'string' ? value : '';
}

function asNumber(value: unknown, fallback: number): number {
	return typeof value === 'number' && Number.isFinite(value) ? value : fallback;
}

/** Read and normalise a cmoa.json. Only the fields the monitor displays are kept. */
export function loadConfig(path: string): MonitorConfig {
	const abs = resolve(path);
	let raw: unknown;
	try {
		raw = JSON.parse(readFileSync(abs, 'utf8'));
	} catch (err) {
		throw new ConfigError(`cannot read config ${abs}: ${(err as Error).message}`);
	}
	const root = asRecord(raw);
	const list = Array.isArray(root.proposers) ? root.proposers : [];
	const proposers: ProposerConfig[] = list.map((entry) => {
		const p = asRecord(entry);
		return {
			id: asString(p.id),
			model: asString(p.model),
			baseUrl: serverBase(asString(p.base_url)),
			maxTokens: asNumber(p.max_tokens, 1024)
		};
	});
	if (proposers.length === 0) throw new ConfigError(`no proposers in ${abs}`);

	const config: MonitorConfig = { path: abs, proposers };

	const judge = asRecord(root.judge);
	if (Object.keys(judge).length > 0) {
		config.judge = {
			baseUrl: serverBase(asString(judge.base_url)),
			model: asString(judge.model) || '?',
			parallel: asNumber(judge.parallel, 6)
		};
	}

	const serve = asRecord(root.serve);
	if (Object.keys(serve).length > 0) {
		const runsDir = asString(serve.runs_dir);
		config.serve = {
			listen: asString(serve.listen),
			// runs_dir is relative to the config file, exactly as CMoA resolves it.
			runsDir: runsDir ? resolve(dirname(abs), runsDir) : ''
		};
	}

	const vault = asString(asRecord(root.harness).vault);
	if (vault) config.vault = vault;

	return config;
}

export interface MonitorEnv {
	configPath: string;
	config: MonitorConfig;
	/** Absolute monitored roots, in the order given. */
	roots: string[];
	calibrationsDir: string | null;
	intervalMs: number;
}

/**
 * Resolve the monitor's settings from the environment.
 * Throws a `ConfigError` with a usable message when `CMOA_CONFIG` is missing,
 * because there is nothing sensible to display without it.
 */
export function readEnv(env: NodeJS.ProcessEnv = process.env): MonitorEnv {
	const configPath = (env.CMOA_CONFIG ?? '').trim();
	if (!configPath) {
		throw new ConfigError(
			'CMOA_CONFIG is not set: point it at a cmoa.json, e.g. CMOA_CONFIG=/srv/cmoa/cmoa.json'
		);
	}
	const config = loadConfig(configPath);

	const rootsRaw = (env.CMOA_MONITOR_ROOTS ?? '').trim();
	const roots = rootsRaw
		? rootsRaw
				.split(':')
				.map((entry) => entry.trim())
				.filter(Boolean)
				.map((entry) => (isAbsolute(entry) ? entry : resolve(entry)))
		: config.serve?.runsDir
			? [config.serve.runsDir]
			: [];

	const calibrationsRaw = (env.CMOA_MONITOR_CALIBRATIONS ?? '').trim();
	const calibrationsDir = calibrationsRaw
		? resolve(calibrationsRaw)
		: config.vault
			? resolve(config.vault, 'spec/calibrations')
			: null;

	const interval = Number(env.CMOA_MONITOR_INTERVAL_MS ?? '');
	const intervalMs =
		Number.isFinite(interval) && interval >= 50 ? Math.floor(interval) : DEFAULT_INTERVAL_MS;

	return { configPath: config.path, config, roots, calibrationsDir, intervalMs };
}
