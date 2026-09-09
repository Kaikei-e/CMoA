import { describe, expect, it } from 'vitest';
import { resolve } from 'node:path';
import { ConfigError, loadConfig, readEnv, serverBase, viaGateway } from './config';
import { fixture } from './testing';

const CONFIG = fixture('config', 'cmoa.json');

describe('viaGateway', () => {
	it('rewrites loopback hosts and leaves others', () => {
		expect(viaGateway('http://127.0.0.1:8081', 'host.docker.internal')).toBe(
			'http://host.docker.internal:8081'
		);
		expect(viaGateway('127.0.0.1:8095', 'host.docker.internal')).toBe('host.docker.internal:8095');
		expect(viaGateway('http://localhost:8090/v1', 'host.docker.internal')).toBe(
			'http://host.docker.internal:8090/v1'
		);
		expect(viaGateway('http://[::1]:8095', 'host.docker.internal')).toBe(
			'http://host.docker.internal:8095'
		);
		expect(viaGateway('http://192.168.1.4:8081', 'host.docker.internal')).toBe(
			'http://192.168.1.4:8081'
		);
		expect(viaGateway('http://127.0.0.1:8081', '')).toBe('http://127.0.0.1:8081');
	});
});

describe('serverBase', () => {
	it('drops one trailing /v1 and any trailing slashes', () => {
		expect(serverBase('http://127.0.0.1:8081/v1')).toBe('http://127.0.0.1:8081');
		expect(serverBase('http://127.0.0.1:8081/v1/')).toBe('http://127.0.0.1:8081');
		expect(serverBase('http://127.0.0.1:8081')).toBe('http://127.0.0.1:8081');
		expect(serverBase('http://host/openai/v1')).toBe('http://host/openai');
	});
});

describe('loadConfig', () => {
	it('normalises proposers, judge, serve and vault', () => {
		const config = loadConfig(CONFIG);
		expect(config.proposers.map((p) => p.id)).toEqual(['granite', 'qwen', 'gemma']);
		expect(config.proposers[0].baseUrl).toBe('http://127.0.0.1:8081');
		expect(config.proposers[0].maxTokens).toBe(512);
		expect(config.judge).toEqual({
			baseUrl: 'http://127.0.0.1:8090',
			model: 'gpt-oss-20b',
			parallel: 6
		});
		expect(config.vault).toBe('/srv/example/vault');
	});

	it('resolves serve.runs_dir against the config file, not the process', () => {
		const config = loadConfig(CONFIG);
		expect(config.serve?.runsDir).toBe(fixture('config', 'serve-runs'));
	});

	it('reports an unreadable config by path', () => {
		expect(() => loadConfig(fixture('config', 'nope.json'))).toThrow(ConfigError);
	});
});

describe('readEnv', () => {
	it('refuses to start without CMOA_CONFIG and says so', () => {
		expect(() => readEnv({})).toThrow(/CMOA_CONFIG/);
	});

	it('defaults the roots to serve.runs_dir', () => {
		const env = readEnv({ CMOA_CONFIG: CONFIG });
		expect(env.roots).toEqual([fixture('config', 'serve-runs')]);
		expect(env.intervalMs).toBe(500);
	});

	it('splits CMOA_MONITOR_ROOTS on colons and makes each absolute', () => {
		const env = readEnv({
			CMOA_CONFIG: CONFIG,
			CMOA_MONITOR_ROOTS: `${fixture('serve-root')}:${fixture('task-coding')}:`
		});
		expect(env.roots).toEqual([fixture('serve-root'), fixture('task-coding')]);
	});

	it('defaults the calibrations directory to <vault>/spec/calibrations', () => {
		const env = readEnv({ CMOA_CONFIG: CONFIG });
		expect(env.calibrationsDir).toBe(resolve('/srv/example/vault/spec/calibrations'));
	});

	it('takes an explicit calibrations directory and interval', () => {
		const env = readEnv({
			CMOA_CONFIG: CONFIG,
			CMOA_MONITOR_CALIBRATIONS: fixture('calibrations'),
			CMOA_MONITOR_INTERVAL_MS: '250'
		});
		expect(env.calibrationsDir).toBe(fixture('calibrations'));
		expect(env.intervalMs).toBe(250);
	});

	it('ignores an unusable interval', () => {
		expect(readEnv({ CMOA_CONFIG: CONFIG, CMOA_MONITOR_INTERVAL_MS: 'soon' }).intervalMs).toBe(500);
		expect(readEnv({ CMOA_CONFIG: CONFIG, CMOA_MONITOR_INTERVAL_MS: '1' }).intervalMs).toBe(500);
	});

	it('reads CMOA_MONITOR_GATEWAY', () => {
		expect(readEnv({ CMOA_CONFIG: CONFIG }).gateway).toBe('');
		expect(readEnv({ CMOA_CONFIG: CONFIG, CMOA_MONITOR_GATEWAY: 'host.docker.internal' }).gateway).toBe(
			'host.docker.internal'
		);
	});
});
