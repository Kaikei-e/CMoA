import { expect, test, type Page } from '@playwright/test';
import { FLEET_PORTS } from './fake-fleet';

const CHAT_SELECTED = '20260101T000000Z-11111111';
const CHAT_NO_CANDIDATE = '20260101T001000Z-22222222';
const CODING = '20260101T003000Z-44444444';

const CONTROL = `http://127.0.0.1:${FLEET_PORTS.control}`;

/** Wait past the boot overlay so the panels underneath are the thing being asserted. */
async function open(page: Page, path = '/'): Promise<void> {
	await page.goto(path);
	await expect(page.getByTestId('boot')).toHaveCount(0, { timeout: 10_000 });
	await expect(page.getByRole('heading', { name: 'Fleet' })).toBeVisible();
}

test('boots, clears the overlay and shows the deck', async ({ page }) => {
	await page.goto('/');
	// The overlay is transient; whether this test catches it or not, it must go.
	await expect(page.getByTestId('boot')).toHaveCount(0, { timeout: 10_000 });

	await expect(page.getByRole('heading', { name: 'Fleet' })).toBeVisible();
	await expect(page.getByRole('heading', { name: 'Propose' })).toBeVisible();
	await expect(page.getByRole('heading', { name: 'Select' })).toBeVisible();
	await expect(page.getByRole('heading', { name: 'Timeline' })).toBeVisible();

	// Every configured server reaches the fake fleet.
	await expect(page.getByTestId('fleet-cell')).toHaveCount(5);
	await expect(page.getByTestId('fleet-cell').filter({ hasText: 'unreachable' })).toHaveCount(0, {
		timeout: 10_000
	});
	await expect(page.getByTestId('link-lamp')).toContainText('LINK LIVE');
});

test('a pinned chat run shows the judge grid, the wins and the selection', async ({ page }) => {
	await open(page, `/?run=${CHAT_SELECTED}`);

	const grid = page.getByTestId('judge-grid');
	await expect(grid).toBeVisible();
	await expect(grid).toContainText('granite|qwen');
	await expect(grid).toContainText('ab ok A>granite 19.1s');
	await expect(grid).toContainText('draw (tie)');
	await expect(grid.getByText('-> gemma')).toHaveCount(2);
	await expect(grid).toContainText('swap-consistent 2/3');
	await expect(grid).toContainText('selected gemma');

	await expect(page.getByTestId('select-sentence')).toHaveText('selected gemma');
	await expect(page.getByTestId('select-sentence')).toHaveAttribute('data-colour', 'ok');
	await expect(page.locator('.chip--pinned')).toContainText('PINNED');

	// A historical run has no fleet sample of its own, and idle is not a fault.
	await expect(page.getByTestId('lane')).toHaveCount(3);
	await expect(page.getByTestId('lane').first()).toContainText('done');
});

test('a run that selected nobody says so in red', async ({ page }) => {
	await open(page, `/?run=${CHAT_NO_CANDIDATE}`);

	const sentence = page.getByTestId('select-sentence');
	await expect(sentence).toHaveText('no_candidate (no_majority)');
	await expect(sentence).toHaveAttribute('data-colour', 'bad');
	await expect(page.getByTestId('judge-grid')).toContainText('no_candidate (no_majority)');
});

test('a coding run shows verify statuses and no judge panel', async ({ page }) => {
	await open(page, `/?run=${CODING}`);

	await expect(page.getByRole('heading', { name: 'Judge' })).toHaveCount(0);
	await expect(page.getByTestId('lane').filter({ hasText: 'verify pass' })).toHaveCount(2);
	await expect(page.getByTestId('lane').filter({ hasText: 'verify fail' })).toHaveCount(1);
	await expect(page.getByTestId('lane').first()).toContainText('1 file(s) +1/-1');
	await expect(page.getByTestId('select-sentence')).toHaveText('selected granite');
});

test('the runs drawer lists the fixtures and a row pins the run', async ({ page }) => {
	await open(page);

	await page.keyboard.press('r');
	const rows = page.getByTestId('run-row');
	await expect(rows).toHaveCount(5);
	await expect(rows.filter({ hasText: CHAT_SELECTED })).toBeVisible();

	await page.locator(`[data-run="${CHAT_NO_CANDIDATE}"]`).click();
	await expect(page).toHaveURL(new RegExp(`\\?run=${CHAT_NO_CANDIDATE}$`));
	await expect(page.getByTestId('select-sentence')).toHaveText('no_candidate (no_majority)');

	await page.keyboard.press('Escape');
	await expect(page.getByTestId('run-row')).toHaveCount(0);
});

test('the inspect drawer reads a candidate body out of the run', async ({ page }) => {
	await open(page, `/?run=${CHAT_SELECTED}`);

	await page.locator('[data-lane="gemma"]').click();
	const body = page.getByTestId('inspect-body');
	await expect(body).toBeVisible();
	await expect(body).toContainText('Candidate answer placeholder for gemma');

	// The picker reaches the judge calls of the same run.
	await page.getByRole('button', { name: 'judge/0-ab.json', exact: true }).click();
	await expect(body).toContainText('"first"');

	await page.keyboard.press('Escape');
	await expect(page.getByTestId('inspect-body')).toHaveCount(0);
});

test('a proposer that goes away turns its fleet cell unreachable', async ({ page, request }) => {
	await open(page);
	const cell = page.locator('[data-server="gemma"]');
	await expect(cell).toContainText('idle');

	await request.post(`${CONTROL}/stop/${FLEET_PORTS.gemma}`);
	try {
		await expect(cell).toContainText('unreachable', { timeout: 5000 });
		await expect(page.locator('[data-server="granite"]')).toContainText('idle');
	} finally {
		await request.post(`${CONTROL}/start/${FLEET_PORTS.gemma}`);
	}
	await expect(cell).toContainText('idle', { timeout: 5000 });
});

test('the calibration tile names the judge and its verdict', async ({ page }) => {
	await open(page);

	const tile = page.getByTestId('calibration-tile').filter({ hasText: 'gpt-oss-20b' });
	await expect(tile).toBeVisible();
	await expect(tile).toContainText('uncalibrated');
	await expect(tile).toContainText('0.281');
	await expect(tile).toContainText(/IN FORCE THROUGH|EXPIRED/);
});
