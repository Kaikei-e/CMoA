<script lang="ts">
	interface Props {
		intervalMs: number;
		sampledAtMs: number | null;
		motion: boolean;
		onmotion: (on: boolean) => void;
		/** One short sentence, replaced only on a discrete change. */
		announcement: string;
	}

	let { intervalMs, sampledAtMs, motion, onmotion, announcement }: Props = $props();

	const KEYS: [string, string][] = [
		['R', 'runs'],
		['I', 'inspect'],
		['F', 'follow'],
		['C', 'comm'],
		['Esc', 'close']
	];

	/**
	 * The key to the four state colours, one word each. Colour on this screen is
	 * redundant with the state word, and this is what makes that promise
	 * checkable — the word alone is always enough to read the screen.
	 */
	const LEGEND: [string, string][] = [
		['c-ok', 'ok'],
		['c-run', 'run'],
		['c-bad', 'bad'],
		['c-dim', 'dim']
	];

	const sampled = $derived(
		sampledAtMs ? new Date(sampledAtMs).toISOString().slice(11, 23) + 'Z' : '--:--:--.---'
	);
</script>

<footer class="status">
	<ul class="keys">
		{#each KEYS as [key, what] (key)}
			<li><kbd>{key}</kbd> <span class="t-3">{what}</span></li>
		{/each}
	</ul>

	<!--
		The only live region on a screen that repaints twice a second: everything
		else is marked aria-hidden or is simply not announced.
	-->
	<p class="announce t-2" aria-live="polite" aria-atomic="true">{announcement}</p>

	<ul class="legend" aria-label="state colours">
		{#each LEGEND as [cls, word] (word)}
			<li>
				<span class="lamp {cls}" aria-hidden="true"></span>
				<span class="word {cls}">{word}</span>
			</li>
		{/each}
	</ul>

	<div class="readouts">
		<button
			type="button"
			class="motion"
			class:motion--off={!motion}
			aria-pressed={motion}
			onclick={() => onmotion(!motion)}
		>
			MOTION {motion ? 'ON' : 'OFF'}
		</button>
		<span class="t-3">interval</span>
		<span class="t-1">{intervalMs || '-'}<span class="unit">ms</span></span>
		<span class="t-3">sample</span>
		<span class="t-1">{sampled}</span>
	</div>
</footer>

<style>
	.status {
		/* the deck packs to the top; the floor stays on the floor */
		margin-top: auto;
		display: flex;
		align-items: center;
		gap: var(--sp-2);
		padding: var(--sp-half) var(--sp);
		background: var(--c-bg-2);
		border: 1px solid var(--c-line-1);
		font-size: 11px;
		min-width: 0;
	}

	.keys {
		display: flex;
		gap: var(--sp);
		list-style: none;
		margin: 0;
		padding: 0;
		flex: none;
	}

	kbd {
		font: inherit;
		border: 1px solid var(--c-line-2);
		padding: 0 var(--sp-half);
		color: var(--c-fg-2);
	}

	.announce {
		margin: 0;
		flex: 1 1 auto;
		min-width: 0;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.legend {
		display: flex;
		align-items: center;
		gap: var(--sp);
		list-style: none;
		margin: 0;
		padding: 0;
		flex: none;
		font-size: 10px;
	}

	.legend li {
		display: flex;
		align-items: center;
		gap: var(--sp-half);
	}

	.readouts {
		display: flex;
		align-items: baseline;
		gap: var(--sp-half);
		flex: none;
	}

	.readouts .t-1 {
		margin-right: var(--sp);
	}

	.motion {
		font-size: 10px;
		letter-spacing: 0.12em;
		border: 1px solid var(--c-line-2);
		padding: 1px var(--sp-half);
		color: var(--c-fg-2);
		margin-right: var(--sp);
	}

	.motion--off {
		color: var(--c-fg-3);
	}

	@media (max-width: 900px) {
		.status {
			flex-wrap: wrap;
			gap: var(--sp);
		}
		.announce {
			order: 3;
			flex-basis: 100%;
		}
	}

	@media (max-width: 700px) {
		.legend {
			display: none;
		}
	}
</style>
