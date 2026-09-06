<script lang="ts">
	import Drawer from './Drawer.svelte';
	import { shortTime, tone } from '$lib/client/format';
	import type { RunSummary } from '$lib/types';

	interface Props {
		runs: RunSummary[];
		currentId: string | null;
		/** True while the view follows the newest run rather than a pinned one. */
		following: boolean;
		onpin: (runId: string) => void;
		onfollow: () => void;
		onclose: () => void;
	}

	let { runs, currentId, following, onpin, onfollow, onclose }: Props = $props();
</script>

<Drawer title="Runs" side="left" {onclose}>
	{#snippet readout()}
		<span class="t-3">rows</span>
		<span class="t-1">{runs.length}</span>
	{/snippet}

	<div class="mode">
		<button
			type="button"
			class="follow"
			class:follow--on={following}
			onclick={onfollow}
			aria-pressed={following}
		>
			FOLLOW NEWEST
		</button>
		<span class="t-3">{following ? 'tracking the newest run' : 'a run is pinned'}</span>
	</div>

	{#if runs.length === 0}
		<p class="empty t-3">NO RUNS UNDER THE MONITORED ROOTS</p>
	{:else}
		<ul class="rows">
			{#each runs as run (run.id)}
				<li>
					<button
						type="button"
						class="row"
						data-testid="run-row"
						data-run={run.id}
						class:row--current={run.id === currentId}
						aria-current={run.id === currentId ? 'true' : undefined}
						onclick={() => onpin(run.id)}
					>
						<span class="row__id t-hi ellipsis">{run.id}</span>
						<span class="row__face t-info">{run.face}</span>
						<span class="row__time t-3">{shortTime(run.createdAt)}</span>
						<span class="row__outcome {tone(run.outcomeColour)} ellipsis">{run.outcome}</span>
						<span class="row__preview t-2">{run.preview}</span>
					</button>
				</li>
			{/each}
		</ul>
	{/if}
</Drawer>

<style>
	.mode {
		display: flex;
		align-items: center;
		gap: var(--sp);
		padding: var(--sp);
		border-bottom: 1px solid var(--c-line-1);
		font-size: 11px;
	}

	.follow {
		flex: none;
		font-size: 10px;
		letter-spacing: 0.12em;
		padding: 2px var(--sp);
		border: 1px solid var(--c-line-2);
		color: var(--c-fg-2);
	}

	.follow--on {
		color: var(--c-sweep);
		border-color: currentColor;
	}

	.rows {
		list-style: none;
		margin: 0;
		padding: 0;
		content-visibility: auto;
		contain-intrinsic-size: auto 64px;
	}

	.row {
		display: grid;
		grid-template-columns: 1fr auto auto;
		grid-template-areas:
			'id face time'
			'outcome outcome outcome'
			'preview preview preview';
		gap: 1px var(--sp);
		width: 100%;
		padding: var(--sp-half) var(--sp) var(--sp-half) calc(var(--sp) - var(--rail));
		border-left: var(--rail) solid transparent;
		border-bottom: 1px solid var(--c-line-1);
		font-size: 11px;
	}

	.row:hover,
	.row:focus-visible {
		background: var(--c-bg-2);
		border-left-color: var(--c-line-2);
	}

	.row--current {
		background: var(--c-bg-3);
		border-left-color: var(--c-sweep);
	}

	.row__id {
		grid-area: id;
		font-size: 12px;
	}

	.row__face {
		grid-area: face;
		text-transform: uppercase;
		letter-spacing: 0.08em;
		font-size: 10px;
	}

	.row__time {
		grid-area: time;
	}

	.row__outcome {
		grid-area: outcome;
	}

	.row__preview {
		grid-area: preview;
		display: -webkit-box;
		-webkit-box-orient: vertical;
		-webkit-line-clamp: 2;
		line-clamp: 2;
		overflow: hidden;
		color: var(--c-fg-3);
	}

	.empty {
		margin: var(--sp);
		font-size: 11px;
		letter-spacing: 0.1em;
	}
</style>
