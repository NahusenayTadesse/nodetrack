<script lang="ts">
  import type { Job } from '../lib/api';
  import { duration, relative, datetime } from '../lib/format';
  import { env } from '../lib/env.svelte';
  import Spinner from './Spinner.svelte';

  let {
    jobs,
    activeId = null,
    onselect
  }: { jobs: Job[]; activeId?: string | null; onselect: (job: Job) => void } = $props();

  const took = (j: Job) =>
    j.finishedAt > 0 ? duration(j.finishedAt - j.startedAt) : j.startedAt ? duration(env.now - j.startedAt) : '–';
</script>

{#if jobs.length}
  <ul class="jobs">
    {#each jobs as j (j.id)}
      <li>
        <button
          type="button"
          class="job {j.status}"
          class:active={j.id === activeId}
          aria-current={j.id === activeId ? 'true' : undefined}
          onclick={() => onselect(j)}
          title="{j.command} — {j.status}{j.exitCode != null ? `, exit ${j.exitCode}` : ''}, started {datetime(j.startedAt)}"
        >
          {#if j.status === 'running'}
            <span class="ind run"><Spinner size={10} label="Running" /></span>
          {:else}
            <span class="ind dot" aria-hidden="true"></span>
          {/if}
          <span class="cmd mono">{j.command}</span>
          <span class="meta num">
            {#if j.exitCode != null && j.status !== 'ok'}<span class="code">exit {j.exitCode}</span>{/if}
            {#if j.status === 'canceled'}<span class="code">canceled</span>{/if}
            <span>{took(j)}</span>
            <span class="when">{relative(j.startedAt, env.now)}</span>
          </span>
        </button>
      </li>
    {/each}
  </ul>
{:else}
  <p class="none">No jobs yet. Installs and script runs show up here.</p>
{/if}

<style>
  .jobs {
    list-style: none;
    margin: 0;
    padding: 4px 0;
  }
  .job {
    display: grid;
    grid-template-columns: 14px minmax(0, 1fr) auto;
    align-items: center;
    gap: 10px;
    width: 100%;
    padding: 7px 16px;
    border: 0;
    background: transparent;
    text-align: left;
    cursor: pointer;
    font-size: 12.5px;
  }
  .job:hover {
    background: var(--surface-2);
  }
  .job.active {
    background: color-mix(in srgb, var(--accent) 9%, transparent);
    box-shadow: inset 2px 0 0 var(--accent);
  }
  .ind {
    display: grid;
    place-items: center;
    color: var(--accent);
  }
  .dot::before {
    content: '';
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--up);
  }
  .failed .dot::before {
    background: var(--down);
  }
  .canceled .dot::before {
    background: var(--warn);
  }
  .cmd {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--text-2);
  }
  .meta {
    display: flex;
    gap: 10px;
    font-size: 11.5px;
    color: var(--muted);
    white-space: nowrap;
  }
  .code {
    color: var(--down);
  }
  .canceled .code {
    color: var(--warn);
  }
  .none {
    padding: 14px 16px;
    font-size: 12.5px;
    color: var(--muted);
  }
  @media (max-width: 480px) {
    .when {
      display: none;
    }
  }
</style>
