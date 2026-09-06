import { FakeFleet } from './fake-fleet';

/**
 * The fake fleet must be listening before the monitor's first poll and must stay
 * up for the whole run, so it belongs to the Playwright process rather than to
 * any one test file. Tests reach it through the control port.
 */
let fleet: FakeFleet | null = null;

export default async function globalSetup(): Promise<() => Promise<void>> {
	fleet = new FakeFleet();
	await fleet.start();
	return async () => {
		await fleet?.stop();
		fleet = null;
	};
}
