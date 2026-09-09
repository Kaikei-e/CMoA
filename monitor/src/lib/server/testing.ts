import { readFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import type { FleetSample, RunRef } from '$lib/types';
import { JUDGE_ID } from './fleet';

// Test-only helpers. Kept out of the *.test.ts glob so several suites can share
// them without vitest trying to run this file.

const here = dirname(fileURLToPath(import.meta.url));

/** An absolute path inside `monitor/tests/fixtures`. */
export function fixture(...parts: string[]): string {
	return resolve(here, '../../../tests/fixtures', ...parts);
}

export function fixtureJson<T = unknown>(...parts: string[]): T {
	return JSON.parse(readFileSync(fixture(...parts), 'utf8')) as T;
}

export function fixtureText(...parts: string[]): string {
	return readFileSync(fixture(...parts), 'utf8');
}

/** A run reference pointing at a fixture run directory. */
export function fixtureRun(root: string, ...parts: string[]): RunRef {
	const dir = fixture(root, ...parts);
	const rootDir = fixture(root);
	const requestDir = parts.length >= 2 && parts[parts.length - 2] === 'runs'
		? join(dir, '..', '..')
		: undefined;
	return {
		id: parts[parts.length - 1] ?? root,
		dir,
		requestDir: requestDir ? resolve(requestDir) : undefined,
		root: rootDir
	};
}

export function proposerSample(id: string, overrides: Partial<FleetSample> = {}): FleetSample {
	return {
		id,
		role: 'proposer',
		baseUrl: `http://127.0.0.1:8081`,
		model: 'a-model',
		mode: 'slots',
		reachable: true,
		processing: 0,
		slotCount: 1,
		promptTokens: 0,
		promptProcessed: 0,
		decoded: 0,
		slots: [0],
		cumulative: false,
		tokPerSec: 0,
		...overrides
	};
}

export function judgeSample(overrides: Partial<FleetSample> = {}): FleetSample {
	return proposerSample(JUDGE_ID, {
		role: 'judge',
		baseUrl: 'http://127.0.0.1:8090',
		model: 'gpt-oss-20b',
		slotCount: 6,
		slots: [],
		...overrides
	});
}

/**
 * A `fetch` stand-in that answers from a table of URL to status and body.
 * `calls` is the URL order; `requests` also keeps what was sent, for a route
 * that has to prove what it forwarded. A route may instead be a function, for a
 * stub that has to fail rather than answer.
 */
export function stubFetch(
	routes: Record<string, { status: number; body: string; headers?: HeadersInit } | (() => never)>
): { fetch: typeof fetch; calls: string[]; requests: { url: string; init?: RequestInit }[] } {
	const calls: string[] = [];
	const requests: { url: string; init?: RequestInit }[] = [];
	const impl = (async (input: RequestInfo | URL, init?: RequestInit) => {
		const url = String(input);
		calls.push(url);
		requests.push({ url, init });
		const route = routes[url];
		if (!route) return new Response('not found', { status: 404 });
		if (typeof route === 'function') return route();
		return new Response(route.body, { status: route.status, headers: route.headers });
	}) as typeof fetch;
	return { fetch: impl, calls, requests };
}
