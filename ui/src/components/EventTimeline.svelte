<script lang="ts">
  import type { AppEvent } from '../lib/api';
  import { datetime, relative } from '../lib/format';
  import { env } from '../lib/env.svelte';

  let { events }: { events: AppEvent[] } = $props();

  const LIMIT = 12;
  let expanded = $state(false);

  const sorted = $derived([...events].sort((a, b) => b.ts - a.ts));
  const shown = $derived(expanded ? sorted : sorted.slice(0, LIMIT));
</script>

{#if sorted.length}
  <ol class="timeline">
    {#each shown as e, i (e.ts + e.kind + i)}
      <li class={e.kind}>
        <span class="node" aria-hidden="true"></span>
        <div class="body">
          <span class="kind">{e.kind}</span>
          {#if e.detail}<span class="detail mono">{e.detail}</span>{/if}
        </div>
        <time class="when num" datetime={new Date(e.ts * 1000).toISOString()} title={datetime(e.ts)}
          >{relative(e.ts, env.now)}</time
        >
      </li>
    {/each}
  </ol>
  {#if sorted.length > LIMIT}
    <button type="button" class="more" onclick={() => (expanded = !expanded)}>
      {expanded ? 'Show fewer' : `Show all ${sorted.length} events`}
    </button>
  {/if}
{:else}
  <p class="none">No events recorded yet.</p>
{/if}

<style>
  .timeline {
    list-style: none;
    margin: 0;
    padding: 6px 0;
  }
  li {
    position: relative;
    display: grid;
    grid-template-columns: 14px minmax(0, 1fr) auto;
    align-items: baseline;
    gap: 12px;
    padding: 7px 16px;
    font-size: 13px;
  }
  /* rail */
  li::before {
    content: '';
    position: absolute;
    left: 22px;
    top: 0;
    bottom: 0;
    width: 1px;
    background: var(--border);
  }
  li:first-child::before {
    top: 50%;
  }
  li:last-child::before {
    bottom: 50%;
  }
  li:only-child::before {
    display: none;
  }
  .node {
    position: relative;
    z-index: 1;
    width: 9px;
    height: 9px;
    margin-left: 2px;
    border-radius: 50%;
    background: var(--surface);
    box-shadow: inset 0 0 0 2px var(--faint);
    align-self: center;
  }
  .start .node {
    background: var(--up);
    box-shadow: none;
  }
  .stop .node {
    box-shadow: inset 0 0 0 2px var(--down);
  }
  .restart .node {
    background: var(--warn);
    box-shadow: none;
  }
  .discovered .node {
    box-shadow: inset 0 0 0 2px var(--info);
  }
  .action .node {
    background: var(--info);
    box-shadow: none;
    border-radius: 2px;
  }
  .job .node {
    box-shadow: inset 0 0 0 2px var(--accent);
    border-radius: 2px;
  }
  .action .kind {
    color: var(--info);
  }
  .body {
    display: flex;
    gap: 10px;
    align-items: baseline;
    min-width: 0;
  }
  .kind {
    font-weight: 600;
    flex: none;
  }
  .stop .kind {
    color: var(--down);
  }
  .restart .kind {
    color: var(--warn);
  }
  .detail {
    color: var(--muted);
    font-size: 12px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .when {
    font-size: 12px;
    color: var(--muted);
    white-space: nowrap;
  }
  .more {
    display: block;
    width: 100%;
    padding: 10px 16px;
    border: 0;
    border-top: 1px solid var(--border);
    background: transparent;
    color: var(--muted);
    font-size: 12.5px;
    cursor: pointer;
    text-align: left;
  }
  .more:hover {
    color: var(--text);
    background: var(--surface-2);
  }
  .none {
    padding: 18px 16px;
    color: var(--muted);
    font-size: 13px;
  }
</style>
