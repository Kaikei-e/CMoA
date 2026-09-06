<script lang="ts">
	import Drawer from './Drawer.svelte';
	import { bytes as formatBytes, prettyJson } from '$lib/client/format';
	import type { InspectState } from '$lib/client/monitor.svelte';
	import type { RunSnapshot } from '$lib/types';

	interface Props {
		run: RunSnapshot | null;
		inspect: InspectState;
		onopen: (path: string) => void;
		onclose: () => void;
	}

	let { run, inspect, onopen, onclose }: Props = $props();

	interface Group {
		label: string;
		paths: string[];
	}

	/**
	 * Every path the run route will hand out for this run, grouped the way an
	 * operator looks for them. Files not written yet stay on the list: asking for
	 * one and being told it is missing is itself information about the round.
	 */
	const groups = $derived.by<Group[]>(() => {
		if (!run?.header.runId) return [];
		const face = run.header.face;
		const out: Group[] = [];

		const round = ['run.json'];
		if (run.select.kind) round.push('select.json');
		if (run.judge) round.push('judge.json');
		if (run.header.requestDir) round.push('conversation.json');
		out.push({ label: 'round', paths: round });

		for (const lane of run.lanes) {
			const paths = [
				`candidates/${lane.id}.${face === 'chat' ? 'txt' : 'diff'}`,
				`candidates/${lane.id}.json`,
				`candidates/${lane.id}.raw.txt`,
				`prompt/${lane.id}.json`
			];
			if (face === 'coding') {
				paths.push(
					`verify/${lane.id}/result.json`,
					`verify/${lane.id}/stdout.txt`,
					`verify/${lane.id}/stderr.txt`
				);
			}
			out.push({ label: lane.id, paths });
		}

		if (run.judge && run.judge.pairs.length > 0) {
			const calls: string[] = [];
			for (const pair of run.judge.pairs) {
				calls.push(`judge/${pair.index}-ab.json`, `judge/${pair.index}-ba.json`);
			}
			out.push({ label: 'judge calls', paths: calls });
		}

		return out;
	});

	const body = $derived(inspect.path ? prettyJson(inspect.path, inspect.content) : '');
</script>

<Drawer title="Inspect" side="right" {onclose}>
	{#snippet readout()}
		{#if inspect.path}
			<span class="t-3">{formatBytes(inspect.bytes)}</span>
			{#if inspect.truncated}<span class="trunc">TRUNCATED</span>{/if}
		{:else}
			<span class="t-3">pick a file</span>
		{/if}
	{/snippet}

	<div class="inspect">
		<nav class="picker" aria-label="trace files">
			{#if groups.length === 0}
				<p class="empty t-3">NO RUN ON SCREEN</p>
			{/if}
			{#each groups as group (group.label)}
				<p class="picker__label t-3">{group.label}</p>
				<ul class="picker__list">
					{#each group.paths as path (path)}
						<li>
							<button
								type="button"
								class="picker__item"
								class:picker__item--on={path === inspect.path}
								aria-current={path === inspect.path ? 'true' : undefined}
								onclick={() => onopen(path)}
							>
								{path}
							</button>
						</li>
					{/each}
				</ul>
			{/each}
		</nav>

		<div class="view">
			<p class="view__path t-2">{inspect.path ?? '-'}</p>
			{#if inspect.loading}
				<p class="empty t-3">READING&hellip;</p>
			{:else if inspect.error}
				<p class="empty c-bad">{inspect.error}</p>
			{:else if inspect.path}
				<pre class="view__body" data-testid="inspect-body">{body}</pre>
			{:else}
				<p class="empty t-3">SELECT A FILE FROM THE LIST</p>
			{/if}
		</div>
	</div>
</Drawer>

<style>
	.inspect {
		display: grid;
		grid-template-rows: minmax(0, 40%) minmax(0, 1fr);
		height: 100%;
		min-height: 0;
	}

	.picker {
		overflow-y: auto;
		border-bottom: 1px solid var(--c-line-2);
		padding-bottom: var(--sp-half);
		scrollbar-width: thin;
	}

	.picker__label {
		margin: var(--sp-half) var(--sp) 2px;
		font-size: 10px;
		text-transform: uppercase;
		letter-spacing: 0.14em;
	}

	.picker__list {
		list-style: none;
		margin: 0;
		padding: 0;
	}

	.picker__item {
		display: block;
		width: 100%;
		padding: 1px var(--sp) 1px calc(var(--sp) - var(--rail));
		border-left: var(--rail) solid transparent;
		font-size: 11px;
		color: var(--c-fg-2);
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}

	.picker__item:hover,
	.picker__item:focus-visible {
		color: var(--c-fg-1);
		background: var(--c-bg-2);
	}

	.picker__item--on {
		color: var(--c-hi);
		background: var(--c-bg-3);
		border-left-color: var(--c-sweep);
	}

	.view {
		display: grid;
		grid-template-rows: auto minmax(0, 1fr);
		min-height: 0;
		background: var(--c-bg-0);
	}

	.view__path {
		margin: 0;
		padding: var(--sp-half) var(--sp);
		font-size: 11px;
		border-bottom: 1px solid var(--c-line-1);
		word-break: break-all;
	}

	.view__body {
		margin: 0;
		padding: var(--sp);
		overflow: auto;
		white-space: pre-wrap;
		overflow-wrap: anywhere;
		font-size: 11px;
		line-height: 1.5;
		color: var(--c-fg-1);
		content-visibility: auto;
		contain-intrinsic-size: auto 480px;
		scrollbar-width: thin;
	}

	.trunc {
		color: var(--c-warn);
		font-size: 10px;
		letter-spacing: 0.12em;
		border: 1px solid currentColor;
		padding: 0 3px;
	}

	.empty {
		margin: var(--sp);
		font-size: 11px;
		letter-spacing: 0.1em;
	}
</style>
