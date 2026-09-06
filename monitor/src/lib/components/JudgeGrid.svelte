<script lang="ts">
	import { fade } from 'svelte/transition';
	import Panel from './Panel.svelte';
	import { oneDecimal, tone } from '$lib/client/format';
	import type { JudgePanel } from '$lib/types';

	interface Props {
		judge: JudgePanel;
		motion: boolean;
		/** Open INSPECT on one judge call file. */
		onopen: (path: string) => void;
	}

	let { judge, motion, onopen }: Props = $props();

	const server = $derived(judge.server);
	const serverColour = $derived(
		!server.reachable ? 'c-bad' : server.busy > 0 ? 'c-run' : 'c-dim'
	);
</script>

<Panel title="Judge">
	{#snippet readout()}
		<span class="t-3">parallel</span>
		<span class="t-1">{judge.parallel}</span>
		<span class="t-3">retries</span>
		<span class={judge.retries > 0 ? 'c-run' : 't-1'}>{judge.retries}</span>
		<span class="t-3">latency</span>
		<span class="t-1">{judge.latencyText}</span>
	{/snippet}

	<div class="server">
		<span class="lamp {serverColour}" class:lamp--off={!server.reachable} aria-hidden="true"></span>
		<span class="t-hi">{judge.model}</span>
		<span class="word {serverColour}">
			{server.reachable ? `${server.busy}/${server.slotCount} busy` : 'unreachable'}
		</span>
		<span class="t-3 ellipsis">slots [{server.slots.length ? server.slots.join(' ') : '-'}]</span>
		<span class="num t-1">{server.decoded}<span class="unit">tok</span></span>
		<span class="num t-1">{oneDecimal(server.tokPerSec)}<span class="unit">tok/s</span></span>
	</div>

	<p class="seed t-2">{judge.seedText}</p>

	<div class="grid scroll-x" data-testid="judge-grid">
		<div class="grid__head" aria-hidden="true">
			<span>pair</span>
			<span>a &rarr; b</span>
			<span>b &rarr; a</span>
			<span>verdict</span>
		</div>

		{#each judge.pairs as pair (pair.index)}
			<div class="row">
				<span class="row__label t-hi ellipsis">{pair.label}</span>
				<button
					type="button"
					class="cell {tone(pair.ab.colour)}"
					onclick={() => onopen(`judge/${pair.index}-ab.json`)}
					title="inspect judge/{pair.index}-ab.json"
				>
					{pair.ab.text}
				</button>
				<button
					type="button"
					class="cell {tone(pair.ba.colour)}"
					onclick={() => onopen(`judge/${pair.index}-ba.json`)}
					title="inspect judge/{pair.index}-ba.json"
				>
					{pair.ba.text}
				</button>
				<span class="row__verdict" class:row__verdict--prov={pair.verdict.provisional}>
					{#key pair.verdict.text}
						<span class={tone(pair.verdict.colour)} in:fade={{ duration: motion ? 220 : 0 }}>
							{pair.verdict.text}
						</span>
					{/key}
					{#if pair.verdict.provisional}
						<span class="prov" title="inferred from the call files; judge.json is not written yet"
							>PROV</span
						>
					{/if}
				</span>
			</div>
		{/each}

		<div class="row row--summary">
			<span class="row__label t-3 word">wins</span>
			<span class="wins">
				{#each judge.wins as win (win.id)}
					<span class="win">
						<span class="t-2">{win.id}</span>
						<span class={win.wins > 0 ? 'c-ok' : 't-3'}>{win.wins}</span>
					</span>
				{/each}
			</span>
			<span class="t-2">
				{#if judge.swapConsistent}
					swap-consistent {judge.swapConsistent.pairs}/{judge.swapConsistent.total}
				{:else}
					<span class="t-3">swap-consistent -</span>
				{/if}
			</span>
		</div>

		<div class="row row--summary">
			<span class="row__label t-3 word">outcome</span>
			<span class="outcome {tone(judge.outcome.colour)}">{judge.outcome.text}</span>
		</div>
	</div>
</Panel>

<style>
	.server {
		display: flex;
		align-items: baseline;
		gap: var(--sp);
		padding: var(--sp-half) var(--sp);
		background: var(--c-bg-3);
		border: 1px solid var(--c-line-2);
		font-size: 12px;
		min-width: 0;
	}

	.server .num {
		margin-left: auto;
	}

	.server .num + .num {
		margin-left: 0;
		min-width: 11ch;
	}

	.seed {
		margin: var(--sp-half) 0 var(--sp);
		font-size: 11px;
	}

	.grid {
		display: grid;
		gap: 1px;
		background: var(--c-line-1);
		border: 1px solid var(--c-line-1);
	}

	.grid__head,
	.row {
		display: grid;
		grid-template-columns: 18ch minmax(23ch, 1fr) minmax(23ch, 1fr) minmax(20ch, 1fr);
		align-items: baseline;
		column-gap: var(--sp);
		padding: 2px var(--sp);
		background: var(--c-bg-2);
		min-width: 0;
	}

	.grid__head {
		font-size: 10px;
		text-transform: uppercase;
		letter-spacing: 0.12em;
		color: var(--c-fg-3);
		background: var(--c-bg-1);
	}

	.row {
		font-size: 12px;
	}

	.row--summary {
		grid-template-columns: 18ch minmax(23ch, 1fr) 1fr;
		background: var(--c-bg-3);
	}

	.cell {
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
		border-bottom: 1px dotted transparent;
	}

	.cell:hover,
	.cell:focus-visible {
		border-bottom-color: currentColor;
	}

	.row__verdict {
		display: flex;
		align-items: baseline;
		gap: var(--sp-half);
		min-width: 0;
	}

	.row__verdict--prov {
		opacity: 0.75;
	}

	.prov {
		font-size: 9px;
		letter-spacing: 0.12em;
		color: var(--c-fg-3);
		border: 1px solid var(--c-line-2);
		padding: 0 3px;
	}

	.wins {
		display: flex;
		gap: var(--sp);
	}

	.win {
		display: inline-flex;
		gap: var(--sp-half);
	}

	.outcome {
		grid-column: 2 / -1;
	}

	@media (max-width: 900px) {
		.grid__head,
		.row {
			grid-template-columns: 14ch repeat(3, minmax(18ch, 1fr));
		}
	}
</style>
