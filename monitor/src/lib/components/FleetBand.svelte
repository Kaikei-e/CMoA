<script lang="ts">
	import { fade } from 'svelte/transition';
	import Panel from './Panel.svelte';
	import { fleetCellState, hostOf, oneDecimal, tone } from '$lib/client/format';
	import type { FleetSample, FleetState } from '$lib/types';

	interface Props {
		fleet: FleetState | null;
		/** Motion is off under the OS preference or the MOTION control. */
		motion: boolean;
	}

	let { fleet, motion }: Props = $props();

	const servers = $derived(fleet?.servers ?? []);
	const proposers = $derived(servers.filter((s) => s.role === 'proposer'));
	const judge = $derived<FleetSample | null>(servers.find((s) => s.id === '__judge__') ?? null);
	const serve = $derived(fleet?.serve ?? null);
	const judgeState = $derived(fleetCellState(judge));
	const reachable = $derived(servers.filter((s) => s.reachable).length);
</script>

<Panel title="Fleet">
	{#snippet readout()}
		<span class="t-3">servers</span>
		<span class={reachable === servers.length ? 'c-ok' : 'c-bad'}>
			{reachable}/{servers.length}
		</span>
	{/snippet}

	<div class="band">
		{#each proposers as sample (sample.id)}
			{@const state = fleetCellState(sample)}
			<div class="cell" data-testid="fleet-cell" data-server={sample.id}>
				<div class="cell__top">
					<span class="lamp {tone(state.colour)}" class:lamp--off={!sample.reachable}></span>
					<span class="cell__id t-hi ellipsis">{sample.id}</span>
					<span class="cell__mode t-3">{sample.mode}</span>
				</div>
				<div class="cell__model t-2 ellipsis" title={sample.model}>{sample.model}</div>
				<div class="cell__line" aria-hidden="true">
					{#key state.word}
						<span class="word {tone(state.colour)}" in:fade={{ duration: motion ? 180 : 0 }}
							>{state.word}</span
						>
					{/key}
					<span class="cell__nums">
						<span class="num t-1">{sample.decoded}<span class="unit">tok</span></span>
						<span class="num t-1">{oneDecimal(sample.tokPerSec)}<span class="unit">tok/s</span></span
						>
					</span>
				</div>
				<p class="sr-only">
					{sample.id}: {state.word}, {sample.decoded} tokens decoded, {oneDecimal(
						sample.tokPerSec
					)} tokens per second
				</p>
				{#if state.prefill}
					<div class="cell__prefill">
						<div class="bar {tone(state.colour)}" aria-hidden="true">
							<span class="bar__fill" style="--v:{state.prefill.fraction}"></span>
							<span class="bar__segments"></span>
						</div>
						<span class="t-3">{state.prefill.done}/{state.prefill.total} prompt tok</span>
					</div>
				{:else}
					<div class="cell__host t-3 ellipsis">{hostOf(sample.baseUrl)}</div>
				{/if}
			</div>
		{/each}

		{#if judge}
			<div class="cell cell--judge" data-testid="fleet-cell" data-server="__judge__">
				<div class="cell__top">
					<span class="lamp {tone(judgeState.colour)}" class:lamp--off={!judge.reachable}></span>
					<span class="cell__id t-hi">JUDGE</span>
				</div>
				<div class="cell__model t-2 ellipsis" title={judge.model}>{judge.model}</div>
				<div class="cell__line">
					<span class="word {tone(judgeState.colour)}">
						{judge.reachable ? `${judge.processing}/${judge.slotCount} busy` : 'unreachable'}
					</span>
					<span class="cell__nums" aria-hidden="true">
						<span class="num t-1">{judge.decoded}<span class="unit">tok</span></span>
						<span class="num t-1">{oneDecimal(judge.tokPerSec)}<span class="unit">tok/s</span></span>
					</span>
				</div>
				<div class="cell__host t-3 ellipsis">
					slots [{judge.slots.length ? judge.slots.join(' ') : '-'}]
				</div>
			</div>
		{/if}

		{#if serve}
			<div class="cell cell--serve" data-testid="fleet-cell" data-server="__serve__">
				<div class="cell__top">
					<span class="lamp {serve.reachable ? 'c-ok' : 'c-bad'}" class:lamp--off={!serve.reachable}
					></span>
					<span class="cell__id t-hi">SERVE</span>
				</div>
				<div class="cell__model t-2 ellipsis">{hostOf(serve.listen)}</div>
				<div class="cell__line">
					<span class="word {serve.reachable ? 'c-ok' : 'c-bad'}">
						{serve.reachable ? 'reachable' : 'unreachable'}
					</span>
				</div>
				<div class="cell__host t-3">GET /v1/models</div>
			</div>
		{/if}
	</div>
</Panel>

<style>
	/*
	 * Flex, not `repeat(auto-fit, …)`: the cells share the row evenly at any count
	 * and wrap without a track template that has to be kept in step with the fleet.
	 */
	.band {
		display: flex;
		flex-wrap: wrap;
		gap: 1px;
		background: var(--c-line-1);
		border: 1px solid var(--c-line-1);
	}

	.cell {
		flex: 1 1 190px;
		display: grid;
		gap: 1px;
		align-content: start;
		padding: var(--sp-half) var(--sp);
		background: var(--c-bg-2);
		min-width: 0;
	}

	.cell--judge,
	.cell--serve {
		background: var(--c-bg-3);
	}

	.cell__top {
		display: flex;
		align-items: center;
		gap: var(--sp-half);
		min-width: 0;
	}

	.cell__id {
		font-size: 13px;
		letter-spacing: 0.04em;
	}

	.cell__mode {
		margin-left: auto;
		font-size: 10px;
		text-transform: uppercase;
		letter-spacing: 0.1em;
	}

	.cell__model,
	.cell__host {
		font-size: 11px;
	}

	.cell__line {
		display: flex;
		align-items: baseline;
		justify-content: space-between;
		gap: var(--sp);
		font-size: 11px;
		min-height: 16px;
		min-width: 0;
	}

	.cell__nums {
		display: flex;
		align-items: baseline;
		gap: var(--sp);
		flex: none;
	}

	.cell__nums .num {
		font-size: 11px;
	}

	.cell__prefill {
		display: grid;
		gap: 2px;
		font-size: 11px;
	}

	@media (max-width: 900px) {
		.cell {
			flex-basis: 150px;
		}
	}
</style>
