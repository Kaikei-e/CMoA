<script lang="ts">
	import { fade } from 'svelte/transition';

	interface Props {
		/** False when the MOTION control is off; the overlay is then skipped too. */
		motion: boolean;
	}

	let { motion }: Props = $props();

	const KEY = 'cmoa-monitor:booted';
	const LINES = [
		'CMoA MONITOR — observation face, comm via serve',
		'SYS CHECK ......... OK',
		'TRACE ROOTS ....... MOUNTED',
		'FLEET PROBE ....... ARMED',
		'LINK .............. OPENING'
	];

	let shown = $state(0);
	let running = $state(false);

	function alreadyBooted(): boolean {
		try {
			return globalThis.sessionStorage?.getItem(KEY) === 'yes';
		} catch {
			// storage can throw outright in a locked-down context; treat that as booted
			return true;
		}
	}

	function markBooted(): void {
		try {
			globalThis.sessionStorage?.setItem(KEY, 'yes');
		} catch {
			// the overlay simply runs again next load
		}
	}

	$effect(() => {
		const reduced =
			typeof matchMedia === 'function' && matchMedia('(prefers-reduced-motion: reduce)').matches;
		if (reduced || !motion || alreadyBooted()) {
			markBooted();
			return;
		}
		running = true;
		markBooted();
		const timers: ReturnType<typeof setTimeout>[] = [];
		LINES.forEach((_, index) => {
			timers.push(setTimeout(() => (shown = index + 1), 120 + index * 150));
		});
		timers.push(setTimeout(() => (running = false), 1050));
		return () => {
			for (const timer of timers) clearTimeout(timer);
		};
	});
</script>

{#if running}
	<div class="boot" out:fade={{ duration: 220 }} data-testid="boot">
		<div class="boot__inner">
			{#each LINES.slice(0, shown) as line, index (line)}
				<p class="boot__line" class:boot__line--first={index === 0}>{line}</p>
			{/each}
			<p class="boot__cursor" aria-hidden="true">&block;</p>
		</div>
	</div>
{/if}

<style>
	.boot {
		position: fixed;
		inset: 0;
		z-index: 60;
		display: grid;
		place-items: center;
		background: var(--c-bg-0);
	}

	.boot__inner {
		width: min(520px, 88vw);
		font-size: 12px;
		line-height: 1.9;
	}

	.boot__line {
		margin: 0;
		color: var(--c-fg-2);
		letter-spacing: 0.08em;
	}

	.boot__line--first {
		color: var(--c-hi);
		letter-spacing: 0.14em;
		margin-bottom: var(--sp);
	}

	.boot__cursor {
		margin: 0;
		color: var(--c-sweep);
	}
</style>
