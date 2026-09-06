<script lang="ts">
	import Panel from './Panel.svelte';
	import { clock, hostOf } from '$lib/client/format';
	import { faultLabel, metaRest, type CommStore } from '$lib/client/comm.svelte';
	import type { ServeHealth } from '$lib/types';

	interface Props {
		comm: CommStore;
		/** `cmoa serve` as the fleet sampler last saw it. */
		serve: ServeHealth | null;
		/** The page's local clock, so the elapsed readout moves during a round. */
		nowMs: number;
		/** Pin the run an answer came from. */
		onpin: (runId: string) => void;
	}

	let { comm, serve, nowMs, onpin }: Props = $props();

	let draft = $state('');
	let transcript: HTMLDivElement | null = $state(null);

	const reachable = $derived(serve?.reachable ?? false);
	const canSend = $derived(reachable && !comm.pending && draft.trim() !== '');
	const elapsed = $derived(comm.startedAtMs ? Math.max(0, (nowMs - comm.startedAtMs) / 1000) : 0);

	// Session storage is read here rather than in the store's constructor: the
	// server renders an empty transcript, and only the browser has the tab's.
	$effect(() => {
		comm.restore();
	});

	// The transcript is read from the bottom, like every other terminal.
	$effect(() => {
		void comm.messages.length;
		void comm.pending;
		transcript?.scrollTo({ top: transcript.scrollHeight });
	});

	function submit(): void {
		if (!canSend) return;
		const text = draft;
		draft = '';
		void comm.send(text);
	}

	function onKey(event: KeyboardEvent): void {
		// Enter sends because this is a chat; Shift+Enter is how a paragraph is
		// written. The window hotkeys already stand aside for a textarea.
		if (event.key !== 'Enter' || event.shiftKey || event.metaKey || event.ctrlKey || event.altKey)
			return;
		event.preventDefault();
		submit();
	}
</script>

<Panel title="Comm">
	{#snippet readout()}
		<span class="t-3">serve</span>
		<span class="t-2" data-testid="comm-target">{serve ? hostOf(serve.listen) : 'none'}</span>
		<span class="lamp {reachable ? 'c-ok' : 'c-bad'}" class:lamp--off={!reachable}></span>
		{#if !reachable}
			<span class="word c-bad" data-testid="comm-offline">SERVE OFFLINE</span>
		{/if}
	{/snippet}

	<div class="comm">
		<div class="transcript" bind:this={transcript} data-testid="comm-transcript">
			{#if comm.messages.length === 0}
				<p class="hint t-3">ASK THE POOL. ONE ROUND PER TURN, WRITTEN AS A TRACE.</p>
			{/if}
			{#each comm.messages as message, index (index)}
				{#if message.role === 'user'}
					<p
						class="line line--user"
						class:line--dropped={message.unanswered}
						data-testid="comm-user"
					>
						<span class="mark" aria-hidden="true">&gt;</span> {message.content}
					</p>
					{#if message.unanswered}
						<p class="meta t-3">unanswered — edit and send it again</p>
					{/if}
				{:else}
					<p class="line line--assistant" data-testid="comm-assistant">{message.content}</p>
					{#if message.run}
						{@const run = message.run}
						<p class="meta t-3" data-testid="comm-meta" title={run.reason}>
							<button type="button" class="runid" onclick={() => onpin(run.id)}>
								run {run.id}
							</button><span>{metaRest(run)}</span>
						</p>
					{/if}
				{/if}
			{/each}
		</div>

		{#if comm.fault}
			{@const fault = comm.fault}
			<p class="fault c-bad" data-testid="comm-fault" role="alert">
				<!-- the label is already upper case; the code is CMoA's own spelling -->
				<span class="fault__label">{faultLabel(fault)}</span>
				{#if fault.runId}
					<button type="button" class="runid" onclick={() => onpin(fault.runId ?? '')}>
						run {fault.runId}
					</button>
				{/if}
				<span class="fault__text">{fault.message}</span>
			</p>
		{/if}

		{#if comm.pending}
			<p class="pending c-run" data-testid="comm-pending">
				<span class="word">ROUND IN PROGRESS</span>
				<span class="num">{clock(elapsed)}</span>
			</p>
		{/if}

		<div class="compose">
			<textarea
				id="comm-input"
				data-testid="comm-input"
				rows="3"
				spellcheck="false"
				placeholder="message the pool — Enter sends, Shift+Enter is a newline"
				aria-label="message to cmoa serve"
				bind:value={draft}
				onkeydown={onKey}
			></textarea>
			<div class="actions">
				<button
					type="button"
					class="act"
					data-testid="comm-clear"
					disabled={comm.pending || comm.messages.length === 0}
					onclick={() => comm.clear()}
				>
					CLEAR
				</button>
				<button
					type="button"
					class="act act--send"
					data-testid="comm-send"
					disabled={!canSend}
					onclick={submit}
				>
					SEND
				</button>
			</div>
		</div>
	</div>
</Panel>

<style>
	.comm {
		display: grid;
		gap: var(--sp-half);
		min-width: 0;
	}

	/*
	 * The transcript is the only thing here that may grow, and it may not push
	 * the deck: it scrolls inside its own frame.
	 */
	.transcript {
		max-height: 40vh;
		overflow-y: auto;
		overflow-x: hidden;
		scrollbar-width: thin;
		scrollbar-color: var(--c-line-2) transparent;
		background: var(--c-bg-2);
		border: 1px solid var(--c-line-1);
		padding: var(--sp-half) var(--sp);
		min-height: 64px;
	}

	.hint {
		margin: 0;
		font-size: 10px;
		letter-spacing: 0.1em;
	}

	.line {
		margin: 0 0 2px;
		font-size: 12px;
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}

	/* a hanging indent, so a wrapped question still reads as one prompt line */
	.line--user {
		color: var(--c-hi);
		padding-left: 2ch;
		text-indent: -2ch;
	}

	.line--user .mark {
		color: var(--c-fg-3);
	}

	.line--dropped {
		color: var(--c-fg-3);
		text-decoration: line-through;
	}

	.line--assistant {
		color: var(--c-fg-1);
		margin-top: var(--sp-half);
	}

	.meta {
		margin: 0 0 var(--sp-half);
		font-size: 10px;
		overflow-wrap: anywhere;
	}

	.runid {
		font-size: inherit;
		color: inherit;
		text-decoration: underline dotted;
		text-underline-offset: 2px;
	}

	.runid:hover,
	.runid:focus-visible {
		color: var(--c-info);
	}

	.fault {
		display: flex;
		flex-wrap: wrap;
		align-items: baseline;
		gap: var(--sp-half);
		margin: 0;
		font-size: 11px;
	}

	.fault__label {
		letter-spacing: 0.06em;
	}

	.fault__text {
		flex-basis: 100%;
		overflow-wrap: anywhere;
		opacity: 0.85;
	}

	.pending {
		display: flex;
		align-items: baseline;
		gap: var(--sp);
		margin: 0;
		font-size: 11px;
	}

	.compose {
		display: grid;
		gap: var(--sp-half);
	}

	textarea {
		font: inherit;
		font-size: 12px;
		resize: vertical;
		width: 100%;
		background: var(--c-bg-2);
		color: var(--c-fg-1);
		border: 1px solid var(--c-line-2);
		padding: var(--sp-half) var(--sp);
	}

	textarea::placeholder {
		color: var(--c-fg-3);
	}

	.actions {
		display: flex;
		justify-content: flex-end;
		gap: var(--sp-half);
	}

	.act {
		font-size: 10px;
		letter-spacing: 0.14em;
		border: 1px solid var(--c-line-2);
		padding: 2px var(--sp);
		color: var(--c-fg-2);
	}

	.act:hover:not(:disabled),
	.act:focus-visible {
		color: var(--c-fg-1);
		border-color: var(--c-fg-3);
	}

	.act--send:not(:disabled) {
		color: var(--c-ok);
		border-color: currentColor;
	}

	.act:disabled {
		color: var(--c-fg-3);
		border-color: var(--c-line-1);
		cursor: not-allowed;
	}
</style>
