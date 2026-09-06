<script lang="ts">
	import Panel from './Panel.svelte';
	import LaneRow from './Lane.svelte';
	import type { Face, Lane } from '$lib/types';

	interface Props {
		lanes: Lane[];
		face: Face;
		phase: string;
		motion: boolean;
		onopen: (laneId: string) => void;
	}

	let { lanes, face, phase, motion, onopen }: Props = $props();

	const budget = $derived(lanes[0]?.maxTokens ?? 0);
</script>

<Panel title="Propose">
	{#snippet readout()}
		<span class="t-3">budget</span>
		<span class="t-1">{budget}<span class="unit">tok</span></span>
		<span class="t-3">lanes</span>
		<span class="t-1">{lanes.length}</span>
	{/snippet}

	<div class="head" aria-hidden="true">
		<span>id</span>
		<span>model</span>
		<span>state</span>
		<span>decoded</span>
		<span class="num">tok</span>
		<span class="num">tok/s</span>
	</div>

	<div class="lanes">
		{#each lanes as lane (lane.id)}
			<LaneRow {lane} {face} {motion} {onopen} />
		{:else}
			<p class="empty t-3">NO PROPOSERS IN THE CONFIGURATION</p>
		{/each}
	</div>

	{#if phase === 'waiting'}
		<p class="empty t-3">WAITING FOR A ROUND</p>
	{/if}
</Panel>

<style>
	.head {
		display: grid;
		grid-template-columns: 9ch minmax(9ch, 15ch) 11ch minmax(90px, 1fr) 9ch 11ch;
		column-gap: var(--sp);
		padding: 0 var(--sp) var(--sp-half);
		font-size: 10px;
		text-transform: uppercase;
		letter-spacing: 0.12em;
		color: var(--c-fg-3);
		border-bottom: 1px solid var(--c-line-2);
	}

	.lanes {
		display: grid;
		gap: 1px;
		background: var(--c-line-1);
		border-bottom: 1px solid var(--c-line-1);
	}

	.empty {
		margin: var(--sp) 0 0;
		font-size: 11px;
		letter-spacing: 0.1em;
	}

	@media (max-width: 900px) {
		.head {
			display: none;
		}
	}
</style>
