<script lang="ts">
	import { basename, clock } from '$lib/client/format';
	import type { SnapshotHeader } from '$lib/types';

	interface Props {
		header: SnapshotHeader;
		/** Seconds, ticked locally between `run` events. */
		elapsedSec: number;
		onFollow: () => void;
	}

	let { header, elapsedSec, onFollow }: Props = $props();

	const phaseColour = $derived(
		header.phase === 'done' ? 'c-ok' : header.phase === 'waiting' ? 'c-dim' : 'c-run'
	);
	const source = $derived(basename(header.requestDir ?? header.dir));
</script>

<div class="round">
	<span class="round__brand">ROUND</span>

	<span class="round__id t-hi ellipsis" title={header.runId ?? 'no run'}>
		{header.runId ?? 'no run under the monitored roots'}
	</span>

	<span class="round__face t-info word">{header.face}</span>

	<dl class="round__facts">
		<dt>elapsed</dt>
		<dd class="t-1">{clock(elapsedSec)}</dd>
		<dt>phase</dt>
		<dd class="word {phaseColour}">{header.phase}</dd>
		<dt>dir</dt>
		<dd class="t-2 ellipsis" title={header.dir ?? ''}>{source}</dd>
	</dl>

	{#if header.mode === 'pinned'}
		<button type="button" class="chip chip--pinned" onclick={onFollow}>
			PINNED <span class="chip__hint">follow &rarr;</span>
		</button>
	{:else}
		<span class="chip chip--follow">FOLLOW</span>
	{/if}
</div>

<style>
	.round {
		display: flex;
		align-items: center;
		gap: var(--sp);
		padding: var(--sp-half) var(--sp);
		background: var(--c-bg-2);
		border: 1px solid var(--c-line-2);
		border-left: var(--rail) solid var(--c-fg-3);
		min-width: 0;
	}

	.round__brand {
		font-size: 11px;
		letter-spacing: 0.18em;
		color: var(--c-fg-3);
	}

	.round__id {
		font-size: 14px;
		letter-spacing: 0.02em;
		flex: 0 1 auto;
	}

	.round__face {
		font-size: 11px;
		border: 1px solid currentColor;
		padding: 0 var(--sp-half);
	}

	.round__facts {
		display: flex;
		align-items: baseline;
		gap: var(--sp-half) var(--sp);
		margin: 0 0 0 auto;
		font-size: 11px;
		min-width: 0;
	}

	.round__facts dt {
		color: var(--c-fg-3);
		text-transform: uppercase;
		letter-spacing: 0.08em;
		font-size: 10px;
	}

	.round__facts dd {
		margin: 0 var(--sp) 0 0;
		max-width: 22ch;
	}

	.chip {
		flex: none;
		font-size: 10px;
		letter-spacing: 0.14em;
		padding: 2px var(--sp-half);
		border: 1px solid currentColor;
		white-space: nowrap;
	}

	.chip--follow {
		color: var(--c-fg-3);
	}

	.chip--pinned {
		color: var(--c-info);
	}

	.chip__hint {
		color: var(--c-fg-3);
		letter-spacing: 0.04em;
	}

	@media (max-width: 900px) {
		.round {
			flex-wrap: wrap;
		}
		.round__facts {
			margin-left: 0;
			flex-wrap: wrap;
		}
	}
</style>
