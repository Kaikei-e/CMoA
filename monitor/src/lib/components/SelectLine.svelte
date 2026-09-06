<script lang="ts">
	import { fade } from 'svelte/transition';
	import Panel from './Panel.svelte';
	import { tone } from '$lib/client/format';
	import type { SelectPanel } from '$lib/types';

	interface Props {
		select: SelectPanel;
		motion: boolean;
		onopen: (path: string) => void;
	}

	let { select, motion, onopen }: Props = $props();
</script>

<Panel title="Select">
	{#snippet readout()}
		{#if select.kind}
			<button type="button" class="file-link t-3" onclick={() => onopen('select.json')}>
				select.json
			</button>
		{:else}
			<span class="t-3">select.json not written</span>
		{/if}
	{/snippet}

	<p
		class="sentence {tone(select.colour)}"
		class:alert-rail={select.colour === 'bad'}
		data-testid="select-sentence"
		data-colour={select.colour}
	>
		{#key select.text}
			<span in:fade={{ duration: motion ? 220 : 0 }}>{select.text}</span>
		{/key}
	</p>

	<dl class="detail">
		<dt>ranked</dt>
		<dd class="t-1">{select.ranked.length ? select.ranked.join(' > ') : '-'}</dd>
		<dt>also passed</dt>
		<dd class="t-2">{select.alsoPassed.length ? select.alsoPassed.join(', ') : '-'}</dd>
	</dl>
</Panel>

<style>
	.sentence {
		margin: 0 0 var(--sp);
		font-size: 17px;
		letter-spacing: 0.02em;
	}

	.detail {
		display: grid;
		grid-template-columns: 12ch 1fr;
		align-items: baseline;
		gap: 2px var(--sp);
		margin: 0;
		font-size: 12px;
	}

	.detail dt {
		color: var(--c-fg-3);
		text-transform: uppercase;
		letter-spacing: 0.1em;
		font-size: 10px;
	}

	.detail dd {
		margin: 0;
		min-width: 0;
		overflow-wrap: anywhere;
	}

	.file-link {
		border-bottom: 1px dotted currentColor;
	}
</style>
