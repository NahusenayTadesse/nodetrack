<script lang="ts">
  import { onMount } from 'svelte';
  import { cancelJob, type Job } from '../lib/api';
  import { JobFeed } from '../lib/jobfeed.svelte';
  import { duration, terminalText, datetime } from '../lib/format';
  import { env } from '../lib/env.svelte';
  import Spinner from './Spinner.svelte';
  import CopyButton from './CopyButton.svelte';

  interface Props {
    /** the job to show; the parent re-keys this component per job id */
    job: Job;
    onclose: () => void;
    onfinished: (job: Job) => void;
  }

  let { job, onclose, onfinished }: Props = $props();

  // `job` only seeds the feed; afterwards the feed owns the job's state.
  // svelte-ignore state_referenced_locally
  const feed = new JobFeed(job, (j) => onfinished(j));

  onMount(() => {
    feed.start();
    return () => feed.destroy();
  });

  // On phones the console opens below the fold; bring it into view once.
  const reveal = (el: HTMLElement) => {
    el.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
  };

  const text = $derived(terminalText(feed.raw));
  const j = $derived(feed.job);
  const elapsed = $derived(
    j.finishedAt > 0 ? j.finishedAt - j.startedAt : j.startedAt ? Math.max(0, env.now - j.startedAt) : 0
  );

  let stick = $state(true);
  let canceling = $state(false);
  let cancelError = $state<string | null>(null);

  async function cancel() {
    canceling = true;
    cancelError = null;
    try {
      feed.update(await cancelJob(j.id));
    } catch (e) {
      cancelError = e instanceof Error ? e.message : String(e);
    } finally {
      canceling = false;
    }
  }

  function onscroll(e: Event) {
    const el = e.currentTarget as HTMLElement;
    stick = el.scrollTop + el.clientHeight >= el.scrollHeight - 12;
  }

  let scroller: HTMLElement | undefined;

  // Follow the tail while the user hasn't scrolled up.
  const autoscroll = (el: HTMLElement) => {
    scroller = el;
    $effect(() => {
      void text;
      if (stick) el.scrollTop = el.scrollHeight;
    });
  };

  function jumpToEnd() {
    stick = true;
    if (scroller) scroller.scrollTop = scroller.scrollHeight;
  }

  const STATUS: Record<Job['status'], string> = {
    running: 'running',
    ok: 'succeeded',
    failed: 'failed',
    canceled: 'canceled'
  };
</script>

<section class="console {j.status}" aria-label="Job output" {@attach reveal}>
  <header class="bar">
    <div class="what">
      <code class="cmd" title={j.command}><span class="prompt" aria-hidden="true">$</span>{j.command}</code>
      <span class="where mono" title="{j.user}@{j.dir}">
        {#if j.user}<span>{j.user}</span>{/if}{#if j.user && j.dir}<span class="dim in">in</span>{/if}{#if j.dir}<span
            >{j.dir}</span
          >{/if}
      </span>
    </div>
    <div class="tools">
      <span class="status" title={j.startedAt ? `Started ${datetime(j.startedAt)}` : ''}>
        {#if j.status === 'running'}
          <Spinner size={11} label="Job running" />
        {:else}
          <span class="led" aria-hidden="true"></span>
        {/if}
        <span>{STATUS[j.status]}</span>
        {#if j.exitCode != null}<span class="dim">· exit {j.exitCode}</span>{/if}
        {#if elapsed > 0 || j.status !== 'running'}<span class="dim num">· {duration(elapsed)}</span>{/if}
      </span>
      {#if j.status === 'running'}
        <button type="button" class="tbtn warn" onclick={cancel} disabled={canceling}>
          {canceling ? 'Canceling…' : 'Cancel'}
        </button>
      {/if}
      {#if text}<span class="copy"><CopyButton text={text} label="Copy output" /></span>{/if}
      <button type="button" class="tbtn icon" onclick={onclose} aria-label="Close console">×</button>
    </div>
  </header>

  {#if j.truncated}
    <p class="notice">Early output was dropped; only the last 512 KiB is kept.</p>
  {/if}
  {#if cancelError}
    <p class="notice err" role="alert">Couldn't cancel: {cancelError}</p>
  {/if}
  {#if feed.error}
    <p class="notice err" role="alert">{feed.error}</p>
  {:else if feed.reconnecting}
    <p class="notice">Reconnecting to the output stream…</p>
  {/if}

  <div class="screen" {onscroll} {@attach autoscroll} tabindex="-1">
    {#if feed.loading}
      <p class="placeholder">Loading output…</p>
    {:else if !text}
      <p class="placeholder">{j.status === 'running' ? 'Waiting for output…' : 'No output.'}</p>
    {:else}
      <pre>{text}</pre>
    {/if}
    {#if j.status === 'running'}<span class="cursor" aria-hidden="true"></span>{/if}
  </div>

  {#if !stick && text}
    <button type="button" class="jump" onclick={jumpToEnd}>
      <svg viewBox="0 0 16 16" aria-hidden="true"><path d="M8 3v10M4 9l4 4 4-4" /></svg>
      Follow output
    </button>
  {/if}
</section>

<style>
  .console {
    color-scheme: dark;
    position: relative;
    display: flex;
    flex-direction: column;
    border: 1px solid var(--term-border);
    border-radius: var(--radius);
    background: var(--term-bg);
    color: var(--term-fg);
    overflow: hidden;
    box-shadow: var(--shadow);
    min-width: 0;
  }
  .bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    flex-wrap: wrap;
    gap: 8px 14px;
    padding: 9px 10px 9px 14px;
    background: var(--term-bar);
    border-bottom: 1px solid var(--term-border);
  }
  .what {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
    flex: 1 1 260px;
  }
  .cmd {
    font-size: 13px;
    font-weight: 600;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .prompt {
    margin-right: 8px;
    color: var(--term-accent);
    user-select: none;
  }
  .where {
    font-size: 11.5px;
    color: var(--term-muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .in {
    margin: 0 6px;
  }
  .dim {
    color: var(--term-muted);
  }
  .tools {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-wrap: wrap;
  }
  .status {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    margin-right: 4px;
    font-family: var(--font-mono);
    font-size: 12px;
    color: var(--term-fg);
  }
  .led {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--term-muted);
  }
  .ok .led {
    background: #4fd88a;
  }
  .ok .status {
    color: #7fe3a9;
  }
  .failed .led {
    background: #ff7a62;
  }
  .failed .status {
    color: #ff9a86;
  }
  .canceled .status {
    color: #f0c27a;
  }
  .canceled .led {
    background: #f0b04a;
  }
  .running .status {
    color: var(--term-accent);
  }
  .tbtn {
    height: 26px;
    padding: 0 10px;
    border-radius: 5px;
    border: 1px solid var(--term-border);
    background: transparent;
    color: var(--term-fg);
    font-size: 12px;
    cursor: pointer;
  }
  .tbtn:hover {
    background: rgba(255, 255, 255, 0.06);
  }
  .tbtn.warn {
    color: #f0c27a;
    border-color: rgba(240, 176, 74, 0.4);
  }
  .tbtn.icon {
    width: 26px;
    padding: 0;
    font-size: 17px;
    line-height: 1;
    color: var(--term-muted);
  }
  .tbtn:disabled {
    opacity: 0.6;
    cursor: progress;
  }
  .copy {
    display: inline-flex;
    --surface-2: rgba(255, 255, 255, 0.06);
    --border: var(--term-border);
    --faint: var(--term-muted);
    --text: var(--term-fg);
  }
  .notice {
    padding: 6px 14px;
    font-size: 12px;
    color: #f0c27a;
    background: rgba(240, 176, 74, 0.08);
    border-bottom: 1px solid var(--term-border);
  }
  .notice.err {
    color: #ff9a86;
    background: rgba(255, 122, 98, 0.08);
  }
  .screen {
    min-height: 140px;
    max-height: clamp(240px, 52vh, 480px);
    overflow: auto;
    padding: 12px 14px 14px;
    outline: none;
    overscroll-behavior: contain;
  }
  pre {
    display: inline;
    margin: 0;
    font-family: var(--font-mono);
    font-size: 12px;
    line-height: 1.55;
    white-space: pre-wrap;
    word-break: break-word;
  }
  .placeholder {
    font-family: var(--font-mono);
    font-size: 12px;
    color: var(--term-muted);
  }
  .cursor {
    display: inline-block;
    width: 7px;
    height: 13px;
    margin-left: 1px;
    vertical-align: text-bottom;
    background: var(--term-accent);
    animation: blink 1s steps(2, start) infinite;
  }
  @keyframes blink {
    to {
      visibility: hidden;
    }
  }
  .jump {
    position: absolute;
    right: 14px;
    bottom: 12px;
    display: inline-flex;
    align-items: center;
    gap: 5px;
    height: 28px;
    padding: 0 11px;
    border-radius: 999px;
    border: 1px solid var(--term-border);
    background: var(--term-bar);
    color: var(--term-fg);
    font-size: 12px;
    cursor: pointer;
    box-shadow: 0 4px 14px rgba(0, 0, 0, 0.35);
  }
  .jump svg {
    width: 12px;
    height: 12px;
    fill: none;
    stroke: currentColor;
    stroke-width: 1.6;
    stroke-linecap: round;
    stroke-linejoin: round;
  }
</style>
