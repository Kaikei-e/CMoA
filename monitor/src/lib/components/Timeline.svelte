<script lang="ts">
	import Panel from './Panel.svelte';
	import { oneDecimal } from '$lib/client/format';
	import type { TimelineEvent } from '$lib/types';

	interface Props {
		events: TimelineEvent[];
		/** Live elapsed, so an unfinished round's axis reaches the present. */
		elapsedSec: number;
		complete: boolean;
	}

	let { events, elapsedSec, complete }: Props = $props();

	/** The axis always ends at the later of the last event and now. */
	const span = $derived(
		Math.max(1, events.length ? events[events.length - 1].offsetSec : 0, complete ? 0 : elapsedSec)
	);

	/** A round 1-2-5 step giving five or six ticks, whatever the round's length. */
	const step = $derived.by(() => {
		const target = span / 5;
		const magnitude = 10 ** Math.floor(Math.log10(Math.max(target, 0.1)));
		for (const factor of [1, 2, 5, 10]) {
			if (magnitude * factor >= target) return magnitude * factor;
		}
		return magnitude * 10;
	});

	const ticks = $derived.by(() => {
		const out: number[] = [];
		for (let at = 0; at <= span + step / 2 && out.length < 24; at += step) {
			out.push(Number(at.toFixed(3)));
		}
		return out;
	});

	function percent(offset: number): number {
		return Math.max(0, Math.min(100, (offset / span) * 100));
	}

	/** Label rows, tallest first. Four is enough for a round's worth of events. */
	const LEVELS = 4;
	/** A monospace glyph against a narrow axis, in percent. Deliberately generous. */
	const CHAR_PCT = 0.85;

	/**
	 * Place each label on the lowest row whose previous label has already ended.
	 * Events cluster (a proposer finishing and the judge starting share a second),
	 * so a fixed odd/even stagger overlaps; this cannot, and it reads no geometry
	 * off the DOM — the width is estimated from the label, which is monospace, and
	 * the span it occupies follows the same anchoring rule the label is drawn with.
	 */
	const placed = $derived.by(() => {
		const ends = new Array<number>(LEVELS).fill(-Infinity);
		return events.map((event) => {
			const left = percent(event.offsetSec);
			const width = (event.label.length + oneDecimal(event.offsetSec).length + 2) * CHAR_PCT;
			const start = left < 6 ? left : left > 94 ? left - width : left - width / 2;
			let level = 0;
			while (level < LEVELS - 1 && ends[level] > start) level += 1;
			ends[level] = start + width + 1;
			return { event, left, level };
		});
	});

	/** Only reserve the label rows actually in use, so a simple round stays short. */
	const rows = $derived(placed.reduce((most, spot) => Math.max(most, spot.level + 1), 1));

	/** Labels at the ends must not hang off the axis. */
	function anchor(pct: number): string {
		if (pct < 6) return 'translateX(0)';
		if (pct > 94) return 'translateX(-100%)';
		return 'translateX(-50%)';
	}
</script>

<Panel title="Timeline">
	{#snippet readout()}
		<span class="t-3">span</span>
		<span class="t-1">{oneDecimal(span)}<span class="unit">s</span></span>
	{/snippet}

	{#if events.length === 0}
		<p class="empty t-3">NO EVENTS YET</p>
	{:else}
		<div class="axis" style="--rows:{rows}" aria-hidden="true">
			{#each placed as spot, index (spot.event.label + index)}
				<div
					class="marker"
					style="left:{spot.left}%; --level:{spot.level}"
					data-final={spot.event.label === 'done' ? 'yes' : null}
				>
					<span class="marker__label" style="transform:{anchor(spot.left)}">
						<span class="marker__name">{spot.event.label}</span>
						<span class="marker__at">{oneDecimal(spot.event.offsetSec)}s</span>
					</span>
					<span class="marker__stem"></span>
					<span class="marker__dot"></span>
				</div>
			{/each}

			{#if !complete}
				<div class="now" style="left:{percent(elapsedSec)}%">
					<span class="now__stem"></span>
				</div>
			{/if}

			<div class="rule"></div>

			{#each ticks as at (at)}
				<div class="tick" style="left:{percent(at)}%">
					<span class="tick__mark"></span>
					<span class="tick__text" style="transform:{anchor(percent(at))}">{oneDecimal(at)}</span>
				</div>
			{/each}
		</div>

		<p class="text t-2">
			{#each events as event, index (event.label + index)}<span class="text__event"
					><span class="t-1">{oneDecimal(event.offsetSec)}</span> {event.label}</span
				>{/each}
		</p>
	{/if}
</Panel>

<style>
	/* `--rows` label rows of 15 px, then the axis, then the tick numbers. */
	.axis {
		--row: 15px;
		--axis-y: calc(var(--rows) * var(--row) + 2px);
		position: relative;
		height: calc(var(--axis-y) + 20px);
		margin: 0 var(--sp) var(--sp);
	}

	.rule {
		position: absolute;
		left: 0;
		right: 0;
		top: var(--axis-y);
		height: 1px;
		background: var(--c-line-2);
	}

	.marker {
		position: absolute;
		top: 0;
		height: calc(var(--axis-y) + 1px);
		width: 0;
	}

	.marker__stem {
		position: absolute;
		left: 0;
		top: calc((var(--rows) - 1 - var(--level)) * var(--row) + 13px);
		bottom: 0;
		width: 1px;
		background: var(--c-line-2);
	}

	.marker__dot {
		position: absolute;
		left: -2px;
		bottom: -2px;
		width: 5px;
		height: 5px;
		background: var(--c-fg-2);
	}

	.marker[data-final='yes'] .marker__dot {
		background: var(--c-ok);
	}

	.marker__label {
		position: absolute;
		top: calc((var(--rows) - 1 - var(--level)) * var(--row));
		display: flex;
		align-items: baseline;
		gap: var(--sp-half);
		line-height: 1.25;
		white-space: nowrap;
	}

	.marker__name {
		font-size: 11px;
		color: var(--c-fg-1);
	}

	.marker__at {
		font-size: 10px;
		color: var(--c-fg-3);
	}

	.now {
		position: absolute;
		top: calc(var(--axis-y) - 12px);
		height: 22px;
		width: 0;
	}

	.now__stem {
		position: absolute;
		left: -1px;
		top: 0;
		bottom: 0;
		width: 2px;
		background: var(--c-warn);
	}

	.tick {
		position: absolute;
		top: calc(var(--axis-y) + 1px);
		width: 0;
	}

	.tick__mark {
		position: absolute;
		left: 0;
		top: 0;
		width: 1px;
		height: 5px;
		background: var(--c-line-2);
	}

	.tick__text {
		position: absolute;
		top: 7px;
		font-size: 10px;
		color: var(--c-fg-3);
		white-space: nowrap;
	}

	.text {
		margin: 0;
		font-size: 11px;
		display: flex;
		flex-wrap: wrap;
		gap: 0 var(--sp-2);
	}

	.text__event::after {
		content: '';
	}

	.empty {
		margin: 0;
		font-size: 11px;
		letter-spacing: 0.1em;
	}
</style>
