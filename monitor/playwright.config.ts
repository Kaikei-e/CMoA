import { defineConfig, devices } from '@playwright/test';

/**
 * The end-to-end suite runs against the built server, pointed at the fixtures:
 * a `cmoa.json` whose proposers are the fake fleet's ports, the fixture serve
 * root and coding task as the monitored roots, and the fixture calibrations.
 *
 * The fake fleet itself is started by `global-setup.ts` and lives for the whole
 * run; a test that needs a server to disappear asks the control port.
 */
const PORT = 4173;

export default defineConfig({
	testDir: 'tests/e2e',
	testMatch: '**/*.e2e.{ts,js}',
	globalSetup: './tests/e2e/global-setup.ts',
	// One shared fake fleet means one worker: a test that stops a server would
	// otherwise pull it out from under a test running beside it.
	workers: 1,
	fullyParallel: false,
	forbidOnly: !!process.env.CI,
	reporter: process.env.CI ? 'list' : [['list']],
	projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
	use: {
		baseURL: `http://127.0.0.1:${PORT}`,
		trace: 'retain-on-failure'
	},
	webServer: {
		// `vite build` is fast and idempotent, so building every run is cheaper
		// than reasoning about whether `build/` is stale.
		command: 'pnpm build && node build',
		port: PORT,
		timeout: 120_000,
		reuseExistingServer: !process.env.CI,
		env: {
			HOST: '127.0.0.1',
			PORT: String(PORT),
			SHUTDOWN_TIMEOUT: '1',
			CMOA_CONFIG: 'tests/fixtures/e2e/cmoa.json',
			CMOA_MONITOR_ROOTS: 'tests/fixtures/serve-root:tests/fixtures/task-coding',
			CMOA_MONITOR_CALIBRATIONS: 'tests/fixtures/calibrations'
		}
	}
});
