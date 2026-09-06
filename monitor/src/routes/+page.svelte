<script lang="ts">
	import { untrack } from 'svelte';
	import { page } from '$app/state';
	import { monitor } from '$lib/client/monitor.svelte';
	import Boot from '$lib/components/Boot.svelte';
	import CalibrationTile from '$lib/components/CalibrationTile.svelte';
	import FleetBand from '$lib/components/FleetBand.svelte';
	import InspectDrawer from '$lib/components/InspectDrawer.svelte';
	import JudgeGrid from '$lib/components/JudgeGrid.svelte';
	import Lanes from '$lib/components/Lanes.svelte';
	import Legend from '$lib/components/Legend.svelte';
	import LinkLamp from '$lib/components/LinkLamp.svelte';
	import Panel from '$lib/components/Panel.svelte';
	import RoundHeader from '$lib/components/RoundHeader.svelte';
	import RunsDrawer from '$lib/components/RunsDrawer.svelte';
	import SelectLine from '$lib/components/SelectLine.svelte';
	import StatusBar from '$lib/components/StatusBar.svelte';
	import Timeline from '$lib/components/Timeline.svelte';

	const pinned = $derived(page.url.searchParams.get('run'));

	// The stream's lifetime is the page's lifetime, and its identity is the URL's.
	// `connect` touches the store's own state, and reading any of it here would
	// make the effect depend on the lamp it sets — a reconnect loop that never
	// settles. The URL is the only dependency this effect may have.
	$effect(() => {
		const runId = pinned;
		untrack(() => monitor.connect(runId));
		return () => monitor.disconnect();
	});

	// The elapsed readout must move between `run` events, which only arrive when
	// the snapshot changes. 100 ms is one tenth of a second on screen.
	$effect(() => {
		const timer = setInterval(() => (monitor.nowMs = Date.now()), 100);
		return () => clearInterval(timer);
	});

	const run = $derived(monitor.run);
	const header = $derived(run?.header ?? null);
	const face = $derived(header?.face ?? 'chat');

	/**
	 * The single sentence the screen announces. It is built from discrete facts
	 * only, so a screen reader hears a round change and nothing at 2 Hz.
	 */
	const announcement = $derived.by(() => {
		if (monitor.error) return `FAULT ${monitor.error}`;
		if (monitor.link === 'lost') return 'LINK LOST — reconnecting';
		if (!header?.runId) return 'NO RUN ON SCREEN';
		return `ROUND ${header.runId} ${header.phase.toUpperCase()} — ${run?.select.text ?? ''}`;
	});

	function candidatePath(laneId: string): string {
		return `candidates/${laneId}.${face === 'chat' ? 'txt' : 'diff'}`;
	}

	function openLane(laneId: string): void {
		void monitor.openFile(candidatePath(laneId));
	}

	function openFile(path: string): void {
		void monitor.openFile(path);
	}

	function onKey(event: KeyboardEvent): void {
		if (event.metaKey || event.ctrlKey || event.altKey) return;
		const target = event.target as HTMLElement | null;
		if (target && (target.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(target.tagName)))
			return;
		switch (event.key) {
			case 'r':
			case 'R':
				monitor.toggleDrawer('runs');
				break;
			case 'i':
			case 'I':
				monitor.toggleDrawer('inspect');
				break;
			case 'f':
			case 'F':
				void monitor.follow();
				break;
			case 'Escape':
				monitor.closeDrawer();
				break;
			default:
				return;
		}
		event.preventDefault();
	}
</script>

<svelte:head><title>CMoA Monitor</title></svelte:head>
<svelte:window onkeydown={onKey} />

<Boot motion={monitor.motion} />

<div class="hud" class:no-motion={!monitor.motion}>
	<header class="rail">
		<h1 class="rail__brand">CMoA <span class="t-3">MONITOR</span></h1>
		<span class="rail__sub t-3">read-only</span>
		<span class="rail__spacer"></span>
		<nav class="rail__actions" aria-label="drawers">
			<button
				type="button"
				class="tab"
				class:tab--on={monitor.drawer === 'runs'}
				aria-pressed={monitor.drawer === 'runs'}
				onclick={() => monitor.toggleDrawer('runs')}
			>
				RUNS <span class="t-3">R</span>
			</button>
			<button
				type="button"
				class="tab"
				class:tab--on={monitor.drawer === 'inspect'}
				aria-pressed={monitor.drawer === 'inspect'}
				onclick={() => monitor.toggleDrawer('inspect')}
			>
				INSPECT <span class="t-3">I</span>
			</button>
		</nav>
		<LinkLamp link={monitor.link} />
	</header>

	{#if monitor.error}
		<div class="fault alert-rail" data-testid="fault">
			<span class="fault__tag">CONFIG FAULT</span>
			<p class="fault__text">{monitor.error}</p>
		</div>
	{/if}

	<FleetBand fleet={monitor.fleet} motion={monitor.motion} />

	<div class="main">
		<div class="column">
			{#if header}
				<RoundHeader {header} elapsedSec={monitor.elapsedSec} onFollow={() => monitor.follow()} />
				<Lanes
					lanes={run?.lanes ?? []}
					{face}
					phase={header.phase}
					motion={monitor.motion}
					onopen={openLane}
				/>
				{#if run?.judge}
					<JudgeGrid judge={run.judge} motion={monitor.motion} onopen={openFile} />
				{/if}
				{#if run}
					<SelectLine select={run.select} motion={monitor.motion} onopen={openFile} />
				{/if}
			{:else}
				<Panel title="Round">
					<p class="waiting t-3">WAITING FOR THE FIRST SNAPSHOT</p>
				</Panel>
			{/if}
		</div>

		<div class="column column--side">
			<CalibrationTile calibration={monitor.calibration} />
			<Panel title="Roots">
				{#snippet readout()}
					<span class="t-3">source</span>
				{/snippet}
				<dl class="roots">
					<dt>run dir</dt>
					<dd class="t-2" title={header?.dir ?? ''}>{header?.dir ?? '-'}</dd>
					<dt>request dir</dt>
					<dd class="t-2" title={header?.requestDir ?? ''}>{header?.requestDir ?? '-'}</dd>
					<dt>calibrations</dt>
					<dd class="t-2" title={monitor.calibration?.dir ?? ''}>
						{monitor.calibration?.dir ?? '-'}
					</dd>
				</dl>
			</Panel>
			<Legend />
		</div>
	</div>

	<Timeline
		events={run?.timeline ?? []}
		elapsedSec={monitor.elapsedSec}
		complete={header?.complete ?? true}
	/>

	<StatusBar
		intervalMs={monitor.intervalMs}
		sampledAtMs={monitor.fleet?.sampledAtMs ?? null}
		motion={monitor.motion}
		onmotion={(on) => monitor.setMotion(on)}
		{announcement}
	/>
</div>

{#if monitor.drawer === 'runs'}
	<RunsDrawer
		runs={monitor.runs}
		currentId={header?.runId ?? null}
		following={header?.mode !== 'pinned'}
		onpin={(id) => monitor.pin(id)}
		onfollow={() => monitor.follow()}
		onclose={() => monitor.closeDrawer()}
	/>
{/if}

{#if monitor.drawer === 'inspect'}
	<InspectDrawer
		{run}
		inspect={monitor.inspect}
		onopen={openFile}
		onclose={() => monitor.closeDrawer()}
	/>
{/if}

<style>
	.rail {
		display: flex;
		align-items: center;
		gap: var(--sp);
		padding: 0 var(--sp-half);
		min-width: 0;
	}

	.rail__brand {
		margin: 0;
		font-size: 14px;
		font-weight: 400;
		letter-spacing: 0.22em;
		color: var(--c-hi);
		white-space: nowrap;
	}

	.rail__sub {
		font-size: 10px;
		letter-spacing: 0.16em;
		text-transform: uppercase;
	}

	.rail__spacer {
		flex: 1 1 auto;
	}

	.rail__actions {
		display: flex;
		gap: var(--sp-half);
	}

	.tab {
		font-size: 10px;
		letter-spacing: 0.14em;
		padding: 2px var(--sp);
		border: 1px solid var(--c-line-2);
		color: var(--c-fg-2);
	}

	.tab:hover,
	.tab:focus-visible {
		color: var(--c-fg-1);
		border-color: var(--c-fg-3);
	}

	.tab--on {
		color: var(--c-sweep);
		border-color: currentColor;
	}

	.fault {
		display: flex;
		align-items: baseline;
		gap: var(--sp);
		padding: var(--sp-half) var(--sp);
		background: var(--c-bg-2);
		border: 1px solid var(--c-alert);
	}

	.fault__tag {
		flex: none;
		font-size: 11px;
		letter-spacing: 0.14em;
		color: var(--c-alert);
	}

	.fault__text {
		margin: 0;
		font-size: 12px;
		color: var(--c-fg-1);
		overflow-wrap: anywhere;
	}

	.main {
		display: grid;
		grid-template-columns: minmax(0, 1fr) 340px;
		gap: var(--sp);
		align-items: start;
		min-width: 0;
	}

	.column {
		display: grid;
		gap: var(--sp);
		align-content: start;
		min-width: 0;
	}

	.roots {
		display: grid;
		grid-template-columns: 12ch 1fr;
		gap: 2px var(--sp);
		margin: 0;
		font-size: 11px;
	}

	.roots dt {
		color: var(--c-fg-3);
		text-transform: uppercase;
		letter-spacing: 0.08em;
		font-size: 10px;
	}

	.roots dd {
		margin: 0;
		min-width: 0;
		overflow-wrap: anywhere;
		display: -webkit-box;
		-webkit-box-orient: vertical;
		-webkit-line-clamp: 2;
		line-clamp: 2;
		overflow: hidden;
	}

	.waiting {
		margin: 0;
		font-size: 11px;
		letter-spacing: 0.1em;
	}

	@media (max-width: 1100px) {
		.main {
			grid-template-columns: minmax(0, 1fr) 280px;
		}
	}

	@media (max-width: 900px) {
		.main {
			grid-template-columns: minmax(0, 1fr);
		}
	}
</style>
