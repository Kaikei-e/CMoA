<script lang="ts">
	import type { LinkState } from '$lib/client/monitor.svelte';

	interface Props {
		link: LinkState;
	}

	let { link }: Props = $props();

	const label = $derived(
		link === 'live' ? 'LINK LIVE' : link === 'lost' ? 'LINK LOST' : 'CONNECTING'
	);
	const cls = $derived(link === 'live' ? 'c-ok' : link === 'lost' ? 'c-bad' : 'c-run');
</script>

<!--
	Only the 3 px rail blinks, and only when the link is down: MIL-STD-1472 keeps
	blink off the glyphs, WCAG keeps it under 3 Hz. 800 ms is the one rate here.
-->
<p
	class="linklamp {cls}"
	data-testid="link-lamp"
	class:alert-rail={link === 'lost'}
	role={link === 'lost' ? 'alert' : undefined}
>
	<span class="lamp" class:lamp--off={link !== 'live'} aria-hidden="true"></span>
	<span class="word">{label}</span>
</p>

<style>
	.linklamp {
		display: flex;
		align-items: center;
		gap: var(--sp-half);
		margin: 0;
		font-size: 11px;
		white-space: nowrap;
	}
</style>
