<script lang="ts">
	import type { Snippet } from 'svelte';

	interface Props {
		title: string;
		side: 'left' | 'right';
		onclose: () => void;
		readout?: Snippet;
		children: Snippet;
	}

	let { title, side, onclose, readout, children }: Props = $props();

	const headingId = $props.id();
</script>

<aside
	class="drawer drawer--{side}"
	aria-labelledby={headingId}
	data-side={side}
	data-testid="drawer-{title.toLowerCase()}"
>
	<header class="drawer__head">
		<h2 class="panel__title" id={headingId}>{title}</h2>
		<span class="panel__rule" aria-hidden="true"></span>
		{#if readout}
			<div class="panel__readout">{@render readout()}</div>
		{/if}
		<button type="button" class="drawer__close" onclick={onclose} aria-label="close {title}">
			ESC &times;
		</button>
	</header>
	<div class="drawer__body">
		{@render children()}
	</div>
</aside>

<style>
	.drawer {
		position: fixed;
		top: 0;
		bottom: 0;
		z-index: 40;
		width: min(460px, 92vw);
		display: grid;
		grid-template-rows: auto minmax(0, 1fr);
		background: var(--c-bg-1);
		box-shadow: 0 0 0 1px var(--c-line-2), 0 0 40px rgba(0, 0, 0, 0.7);
	}

	.drawer--left {
		left: 0;
		border-right: 1px solid var(--c-line-2);
		animation: slide-left 180ms ease-out;
	}

	.drawer--right {
		right: 0;
		border-left: 1px solid var(--c-line-2);
		animation: slide-right 180ms ease-out;
	}

	@keyframes slide-left {
		from {
			transform: translateX(-16px);
			opacity: 0;
		}
	}

	@keyframes slide-right {
		from {
			transform: translateX(16px);
			opacity: 0;
		}
	}

	.drawer__head {
		display: flex;
		align-items: center;
		gap: 0.6em;
		padding: var(--sp);
		border-bottom: 1px solid var(--c-line-2);
		background: var(--c-bg-2);
	}

	.drawer__close {
		flex: none;
		font-size: 10px;
		letter-spacing: 0.12em;
		color: var(--c-fg-3);
		border: 1px solid var(--c-line-2);
		padding: 1px var(--sp-half);
	}

	.drawer__close:hover,
	.drawer__close:focus-visible {
		color: var(--c-fg-1);
	}

	.drawer__body {
		overflow-y: auto;
		overflow-x: hidden;
		scrollbar-width: thin;
		scrollbar-color: var(--c-line-2) transparent;
	}
</style>
