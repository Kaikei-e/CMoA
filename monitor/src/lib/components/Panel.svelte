<script lang="ts">
	import type { Snippet } from 'svelte';

	interface Props {
		/** The bracketed section label, e.g. `PROPOSE`. Brackets are decoration. */
		title: string;
		/** A right-aligned readout on the header rule. */
		readout?: Snippet;
		children: Snippet;
		/** Extra classes on the frame, for grid placement. */
		class?: string;
	}

	let { title, readout, children, class: extra = '' }: Props = $props();

	// The heading needs an id so the section is named without repeating the text.
	const headingId = $props.id();
</script>

<section class="panel {extra}" aria-labelledby={headingId}>
	<span class="panel__tick panel__tick--tl" aria-hidden="true"></span>
	<span class="panel__tick panel__tick--tr" aria-hidden="true"></span>
	<span class="panel__tick panel__tick--bl" aria-hidden="true"></span>
	<span class="panel__tick panel__tick--br" aria-hidden="true"></span>

	<div class="panel__head">
		<h2 class="panel__title" id={headingId}>{title}</h2>
		<span class="panel__rule" aria-hidden="true"></span>
		{#if readout}
			<div class="panel__readout">{@render readout()}</div>
		{/if}
	</div>

	<div class="panel__body">
		{@render children()}
	</div>
</section>
