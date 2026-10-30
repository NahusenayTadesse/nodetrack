<script lang="ts">
  import type { Process } from '../lib/api';
  import { bytes, duration, pct, datetime } from '../lib/format';
  import { env } from '../lib/env.svelte';

  let { processes }: { processes: Process[] } = $props();

  const rows = $derived([...processes].sort((a, b) => a.ppid - b.ppid || a.pid - b.pid));
  const pids = $derived(new Set(processes.map((p) => p.pid)));
</script>

{#if rows.length}
  <div class="scroller">
    <table>
      <thead>
        <tr>
          <th class="r">pid</th>
          <th class="r">ppid</th>
          <th>command</th>
          <th class="r">cpu</th>
          <th class="r">rss</th>
          <th class="r">thr</th>
          <th class="r">core</th>
          <th class="r">started</th>
        </tr>
      </thead>
      <tbody>
        {#each rows as p (p.pid)}
          <tr class:child={pids.has(p.ppid)}>
            <td class="r num strong">{p.pid}</td>
            <td class="r num muted">{p.ppid}</td>
            <td class="cmd mono" title={p.cmd}>{p.cmd}</td>
            <td class="r num">{pct(p.cpu)}</td>
            <td class="r num">{bytes(p.rss)}</td>
            <td class="r num">{p.threads}</td>
            <td class="r num">{p.core}</td>
            <td class="r num muted" title={datetime(p.startedAt)}>
              {p.startedAt ? duration(env.now - p.startedAt) : '–'}
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
{:else}
  <p class="none">No live processes. The app is not running.</p>
{/if}

<style>
  .scroller {
    overflow-x: auto;
    -webkit-overflow-scrolling: touch;
  }
  table {
    width: 100%;
    border-collapse: collapse;
    font-size: 12.5px;
    white-space: nowrap;
  }
  th {
    position: sticky;
    top: 0;
    padding: 8px 12px;
    text-align: left;
    font-size: 10.5px;
    font-weight: 600;
    letter-spacing: 0.07em;
    text-transform: uppercase;
    color: var(--muted);
    background: var(--surface-2);
    border-bottom: 1px solid var(--border);
  }
  td {
    padding: 8px 12px;
    border-bottom: 1px solid var(--border);
  }
  tbody tr:last-child td {
    border-bottom: 0;
  }
  tbody tr:hover {
    background: var(--surface-2);
  }
  .r {
    text-align: right;
  }
  .strong {
    font-weight: 600;
  }
  .cmd {
    max-width: 420px;
    min-width: 180px;
    overflow: hidden;
    text-overflow: ellipsis;
    color: var(--text-2);
  }
  .child .cmd::before {
    content: '└ ';
    color: var(--faint);
  }
  .none {
    padding: 18px 16px;
    color: var(--muted);
    font-size: 13px;
  }
</style>
