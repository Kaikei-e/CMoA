<script lang="ts">
	import Panel from './Panel.svelte';

	/**
	 * The key to the four state colours. It is not decoration: colour on this
	 * screen is redundant with the state word, and the legend is what makes that
	 * promise checkable — the word alone is always enough to read the screen.
	 */
	const ROWS: [string, string, string][] = [
		['c-ok', 'ok', 'pass · done · selected'],
		['c-run', 'run', 'generating · prefill · busy'],
		['c-bad', 'bad', 'failed · timeout · unreachable'],
		['c-dim', 'dim', 'idle · pend · not written']
	];
</script>

<Panel title="Legend">
	<ul class="legend">
		{#each ROWS as [cls, word, words] (word)}
			<li class="legend__row">
				<span class="lamp {cls}" aria-hidden="true"></span>
				<span class="word {cls}">{word}</span>
				<span class="t-3">{words}</span>
			</li>
		{/each}
	</ul>
	<p class="note t-3">Colour repeats the state word. It never carries meaning alone.</p>
</Panel>

<style>
	.legend {
		list-style: none;
		margin: 0;
		padding: 0;
		display: grid;
		gap: 2px;
	}

	.legend__row {
		display: grid;
		grid-template-columns: 10px 5ch 1fr;
		align-items: baseline;
		gap: var(--sp-half);
		font-size: 11px;
		min-width: 0;
	}

	.legend__row .t-3 {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.note {
		margin: var(--sp) 0 0;
		font-size: 10px;
		line-height: 1.5;
	}
</style>
