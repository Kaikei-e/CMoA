<script lang="ts">
	import favicon from '$lib/assets/favicon.svg';
	import '$lib/styles/hud.css';

	let { children } = $props();
</script>

<svelte:head>
	<link rel="icon" href={favicon} />
	<meta name="color-scheme" content="dark" />
</svelte:head>

{@render children()}
