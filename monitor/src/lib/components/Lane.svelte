<script lang="ts">
	import { fade } from 'svelte/transition';
	import { oneDecimal, tone } from '$lib/client/format';
	import type { Face, Lane } from '$lib/types';

	interface Props {
		lane: Lane;
		face: Face;
		motion: boolean;
		/** Open INSPECT on this proposer's candidate. */
		onopen: (laneId: string) => void;
	}

	let { lane, face, motion, onopen }: Props = $props();

	// `live: false` means the fleet has no sample for this id — a historical run.
	// That is idleness, not a fault, so it must never be painted red.
	const stateWord = $derived(lane.live || lane.candidate ? lane.state : 'idle');
	const stateColour = $derived(lane.live || lane.candidate ? lane.colour : 'dim');
</script>

<button
	type="button"
	class="lane"
	data-testid="lane"
	data-lane={lane.id}
	class:lane--fault={lane.candidate ? lane.candidate.colour === 'bad' : lane.colour === 'bad'}
	onclick={() => onopen(lane.id)}
	title="inspect {lane.id}"
>
	<span class="lane__id t-hi">{lane.id}</span>
	<span class="lane__model t-2 ellipsis">{lane.model}</span>
	<span class="lane__state">
		{#key stateWord}
			<span class="word {tone(stateColour)}" in:fade={{ duration: motion ? 180 : 0 }}
				>{stateWord}</span
			>
		{/key}
	</span>
	<span class="lane__bar bar {tone(stateColour)}" aria-hidden="true">
		<span class="bar__fill" style="--v:{lane.barFraction}"></span>
		<span class="bar__segments"></span>
	</span>
	<span class="lane__tok num t-1">{lane.decoded}<span class="unit">tok</span></span>
	<span class="lane__rate num t-1">{oneDecimal(lane.tokPerSec)}<span class="unit">tok/s</span></span>

	<span class="lane__second" class:lane__second--note={!lane.candidate}>
		{#if lane.candidate}
			<span class="tag {tone(lane.candidate.colour)}">cand {lane.candidate.status}</span>
			<span class="lane__info t-1 ellipsis">{lane.candidate.info || '-'}</span>
			<span class="lane__timing t-3">{lane.candidate.timing}</span>
			{#if face === 'chat'}
				<span class="lane__extra t-2">
					reas {lane.candidate.reasoningBytes}B &middot; {lane.candidate.completionTokens} tok
				</span>
			{:else if lane.verify}
				<span class="tag lane__extra {tone(lane.verify.colour)}">verify {lane.verify.status}</span>
			{:else}
				<span class="lane__extra t-3">verify -</span>
			{/if}
		{:else}
			<span class="lane__note t-3">{lane.note}</span>
		{/if}
	</span>
</button>

<style>
	.lane {
		display: grid;
		grid-template-columns:
			[id] 9ch [model] minmax(9ch, 15ch) [state] 11ch
			[bar] minmax(90px, 1fr) [tok] 9ch [rate] 11ch;
		align-items: center;
		column-gap: var(--sp);
		row-gap: 2px;
		width: 100%;
		padding: var(--sp-half) var(--sp) var(--sp-half) calc(var(--sp) - var(--rail));
		border-left: var(--rail) solid transparent;
		background: var(--c-bg-2);
		transition: background 150ms ease;
	}

	.lane:hover,
	.lane:focus-visible {
		background: var(--c-bg-3);
		border-left-color: var(--c-sweep);
	}

	.lane--fault {
		border-left-color: var(--c-alert);
	}

	.lane__id {
		font-size: 13px;
	}

	.lane__model,
	.lane__state {
		font-size: 11px;
	}

	.lane__bar {
		height: 8px;
	}

	.lane__tok,
	.lane__rate {
		font-size: 12px;
	}

	/* Fixed columns, so the timing and the verdict of every lane line up. */
	.lane__second {
		grid-column: model / -1;
		display: grid;
		grid-template-columns: 10ch minmax(0, 1fr) 17ch 21ch;
		align-items: baseline;
		column-gap: var(--sp);
		min-width: 0;
		font-size: 11px;
	}

	.lane__second--note {
		grid-template-columns: 1fr;
	}

	.lane__timing,
	.lane__extra {
		white-space: nowrap;
	}

	.tag {
		flex: none;
		white-space: nowrap;
		text-transform: uppercase;
		letter-spacing: 0.06em;
	}

	.lane__note {
		text-transform: uppercase;
		letter-spacing: 0.06em;
	}

	/*
	 * The bar narrows but never disappears: a `display: none` grid item stops
	 * being an item at all, and every column after it slides one place left.
	 */
	@media (max-width: 900px) {
		.lane {
			grid-template-columns: [id] 8ch [model] minmax(0, 1fr) [state] 10ch [bar] 40px [tok] 9ch [rate] 10ch;
		}
		.lane__second {
			grid-template-columns: 10ch minmax(0, 1fr) 17ch;
		}
		.lane__extra {
			grid-column: 1 / -1;
		}
	}
</style>
