<script lang="ts">
	import Panel from './Panel.svelte';
	import type { Calibration, CalibrationState } from '$lib/types';

	interface Props {
		calibration: CalibrationState | null;
	}

	let { calibration }: Props = $props();

	const entries = $derived(calibration?.entries ?? []);

	/**
	 * `calibrated` is the only good verdict and `uncalibrated` the only bad one;
	 * anything else a document may say is in-between, not unknown.
	 */
	function verdictClass(verdict: string): string {
		if (verdict === 'calibrated') return 'c-ok';
		if (verdict === 'uncalibrated') return 'c-bad';
		return 'c-run';
	}

	function kappa(value: number | null): string {
		return value === null ? '-' : value.toFixed(3);
	}

	function force(entry: Calibration): { text: string; cls: string } {
		if (!entry.inForceUntil) return { text: 'NO EXPIRY ON FILE', cls: 't-3' };
		return entry.inForce
			? { text: `IN FORCE THROUGH ${entry.inForceUntil}`, cls: 'c-ok' }
			: { text: `EXPIRED ${entry.inForceUntil}`, cls: 'c-bad' };
	}
</script>

<Panel title="Calibration">
	{#snippet readout()}
		<span class="t-3">judges</span>
		<span class="t-1">{entries.length}</span>
	{/snippet}

	{#if entries.length === 0}
		<p class="empty t-3">NO CALIBRATION ON FILE</p>
	{:else}
		<ul class="tiles">
			{#each entries as entry (entry.id)}
				{@const inForce = force(entry)}
				<li
					class="tile"
					class:tile--bad={entry.verdict === 'uncalibrated'}
					data-testid="calibration-tile"
				>
					<div class="tile__head">
						<span class="t-hi ellipsis">{entry.judge}</span>
						<span class="word {verdictClass(entry.verdict)}">{entry.verdict}</span>
					</div>
					<dl class="tile__stats">
						<dt>human &kappa;</dt>
						<dd class="t-1">
							{kappa(entry.humanKappa)}<span class="unit">n={entry.nHuman ?? '-'}</span>
						</dd>
						<dt>swap &kappa;</dt>
						<dd class="t-1">{kappa(entry.swapKappa)}</dd>
						<dt>rerun &kappa;</dt>
						<dd class="t-1">{kappa(entry.rerunKappa)}</dd>
						<dt>ties</dt>
						<dd class="t-2 ellipsis">{entry.tieHandling ?? '-'}</dd>
					</dl>
					<p class="tile__force {inForce.cls}">{inForce.text}</p>
				</li>
			{/each}
		</ul>
	{/if}
</Panel>

<style>
	.tiles {
		list-style: none;
		margin: 0;
		padding: 0;
		display: grid;
		gap: var(--sp);
	}

	.tile {
		padding: var(--sp-half) var(--sp);
		background: var(--c-bg-2);
		border: 1px solid var(--c-line-2);
		border-left: var(--rail) solid var(--c-fg-3);
		min-width: 0;
	}

	.tile--bad {
		border-left-color: var(--c-alert);
	}

	.tile__head {
		display: flex;
		align-items: baseline;
		justify-content: space-between;
		gap: var(--sp);
		font-size: 12px;
		margin-bottom: var(--sp-half);
	}

	.tile__head .word {
		font-size: 11px;
		flex: none;
	}

	.tile__stats {
		display: grid;
		grid-template-columns: 9ch 1fr;
		align-items: baseline;
		gap: 1px var(--sp);
		margin: 0;
		font-size: 11px;
	}

	.tile__stats dt {
		color: var(--c-fg-3);
		text-transform: uppercase;
		letter-spacing: 0.08em;
		font-size: 10px;
	}

	.tile__stats dd {
		margin: 0;
		min-width: 0;
	}

	.tile__force {
		margin: var(--sp-half) 0 0;
		font-size: 10px;
		letter-spacing: 0.1em;
	}

	.empty {
		margin: 0;
		font-size: 11px;
		letter-spacing: 0.1em;
	}
</style>
