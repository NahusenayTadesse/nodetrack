<script lang="ts">
  import { memOf, type AppSummary } from '../lib/api';
  import { bytes, pct } from '../lib/format';
  import { live } from '../lib/live.svelte';
  import HostStrip from '../components/HostStrip.svelte';
  import AppRow from '../components/AppRow.svelte';

  type SortKey = 'name' | 'cpu' | 'mem';
  const SORTS: { key: SortKey; label: string }[] = [
    { key: 'name', label: 'Name' },
    { key: 'cpu', label: 'CPU' },
    { key: 'mem', label: 'Memory' }
  ];
  const STORE_KEY = 'nodetrack.sort';

  function loadSort(): SortKey {
    try {
      const v = localStorage.getItem(STORE_KEY);
      if (v === 'name' || v === 'cpu' || v === 'mem') return v;
    } catch {
      /* storage unavailable */
    }
    return 'name';
  }

  let sort = $state<SortKey>(loadSort());

  function setSort(k: SortKey) {
    sort = k;
    try {
      localStorage.setItem(STORE_KEY, k);
    } catch {
      /* ignore */
    }
  }

  const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: 'base' });
  const byName = (a: AppSummary, b: AppSummary) => collator.compare(a.name, b.name) || a.id - b.id;

  const apps = $derived.by(() => {
    const list = [...(live.data?.apps ?? [])];
    list.sort((a, b) => {
      if (a.status !== b.status) return a.status === 'up' ? -1 : 1;
      if (sort === 'cpu') return b.cpu - a.cpu || byName(a, b);
      if (sort === 'mem') return memOf(b) - memOf(a) || byName(a, b);
      return byName(a, b);
    });
    return list;
  });

  const totals = $derived.by(() => {
    let up = 0,
      cpu = 0,
      mem = 0;
    for (const a of apps) {
      if (a.status !== 'up') continue;
      up++;
      cpu += a.cpu;
      mem += memOf(a);
    }
    return { up, down: apps.length - up, cpu, mem };
  });
</script>

<svelte:head>
  <title>{live.data ? `${live.data.host.hostname} · nodetrack` : 'nodetrack'}</title>
</svelte:head>

{#if live.data}
  <div class="page">
    <HostStrip host={live.data.host} tracker={live.data.tracker} />

    <section class="apps" aria-labelledby="apps-h">
      <header class="apps-head">
        <div class="heading">
          <h2 id="apps-h">Apps</h2>
          {#if apps.length}
            <p class="summary num">
              <span>{totals.up} up</span>
              {#if totals.down}<span class="down">{totals.down} down</span>{/if}
              <span class="faint">·</span>
              <span title="Combined CPU of running apps, % of one core">{pct(totals.cpu)} cpu</span>
              <span class="faint">·</span>
              <span title="Combined memory of running apps">{bytes(totals.mem)}</span>
            </p>
          {/if}
        </div>
        {#if apps.length > 1}
          <div class="seg" role="radiogroup" aria-label="Sort apps by">
            {#each SORTS as s (s.key)}
              <button type="button" role="radio" aria-checked={sort === s.key} onclick={() => setSort(s.key)}
                >{s.label}</button
              >
            {/each}
          </div>
        {/if}
      </header>

      {#if apps.length}
        <div class="table" role="list">
          <div class="thead" aria-hidden="true">
            <span>app</span>
            <span class="r">cpu</span>
            <span class="r">memory</span>
            <span>uptime</span>
            <span class="r">restarts</span>
            <span class="r">cores</span>
          </div>
          {#each apps as app (app.id)}
            <div role="listitem"><AppRow {app} cpus={live.data.host.cpus} /></div>
          {/each}
        </div>
      {:else}
        <div class="empty">
          <svg viewBox="0 0 64 40" class="empty-art" aria-hidden="true">
            <polyline points="2,30 16,30 22,12 30,36 36,22 44,22 62,22" pathLength="100" />
          </svg>
          <h3>No apps discovered yet</h3>
          <p>
            nodetrack finds apps on its own. As soon as a <code>node</code> or <code>bun</code> process listens on a
            port, or runs under <code>pm2</code> or <code>systemd</code>, it shows up here with its CPU, memory, ports
            and the nginx / Caddy domains that route to it.
          </p>
          <p class="faint">Nothing to configure. Start an app and give it one collection tick.</p>
        </div>
      {/if}
    </section>
  </div>
{:else if live.error && live.conn !== 'live'}
  <div class="state">
    <h2>Can't reach the nodetrack API</h2>
    <p class="muted mono">{live.error}</p>
    <p class="muted">Retrying in the background. This page updates on its own once the server answers.</p>
  </div>
{:else}
  <div class="state" aria-busy="true">
    <div class="loader" aria-hidden="true"></div>
    <p class="muted">Connecting…</p>
  </div>
{/if}

<style>
  .page {
    display: flex;
    flex-direction: column;
    gap: 22px;
  }
  .apps {
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--surface);
    box-shadow: var(--shadow);
    overflow: hidden;
    --cols: minmax(0, 1fr) 170px 170px 84px 64px 72px;
    --colgap: 20px;
  }
  .apps-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    flex-wrap: wrap;
    padding: 14px 16px;
  }
  .heading {
    display: flex;
    align-items: baseline;
    gap: 12px;
    flex-wrap: wrap;
    min-width: 0;
  }
  h2 {
    font-size: 15px;
    font-weight: 600;
  }
  .summary {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
    font-size: 12px;
    color: var(--muted);
  }
  .summary .down {
    color: var(--down);
  }
  .thead {
    display: grid;
    grid-template-columns: var(--cols);
    gap: var(--colgap);
    padding: 8px 16px;
    border-top: 1px solid var(--border);
    background: var(--surface-2);
    font-size: 10.5px;
    font-weight: 600;
    letter-spacing: 0.07em;
    text-transform: uppercase;
    color: var(--muted);
  }
  .thead .r {
    text-align: right;
  }
  /* cpu / memory headers sit over the numbers, not the sparkline */
  .thead span:nth-child(2),
  .thead span:nth-child(3) {
    text-align: left;
    padding-left: 0;
  }
  .empty {
    border-top: 1px solid var(--border);
    padding: 48px 24px 52px;
    text-align: center;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 10px;
  }
  .empty h3 {
    font-size: 16px;
    font-weight: 600;
  }
  .empty p {
    max-width: 520px;
    color: var(--text-2);
    text-wrap: pretty;
  }
  .empty p.faint {
    color: var(--faint);
    font-size: 13px;
  }
  .empty code {
    padding: 1px 5px;
    border-radius: 4px;
    background: var(--surface-2);
    border: 1px solid var(--border);
  }
  .empty-art {
    width: 96px;
    height: 60px;
    margin-bottom: 4px;
  }
  .empty-art polyline {
    fill: none;
    stroke: var(--accent);
    stroke-width: 2.2;
    stroke-linecap: round;
    stroke-linejoin: round;
    stroke-dasharray: 100;
    stroke-dashoffset: 100;
    animation: draw 2.8s ease-in-out infinite;
  }
  @keyframes draw {
    45%,
    70% {
      stroke-dashoffset: 0;
    }
    100% {
      stroke-dashoffset: -100;
    }
  }
  .state {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 10px;
    padding: 96px 16px;
    text-align: center;
  }
  .state h2 {
    font-size: 17px;
  }
  .loader {
    width: 28px;
    height: 28px;
    border-radius: 50%;
    border: 2px solid var(--border-strong);
    border-top-color: var(--accent);
    animation: spin 0.8s linear infinite;
  }
  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }

  @media (max-width: 1020px) {
    .apps {
      --cols: minmax(0, 1fr) 140px 140px 72px 60px 64px;
      --colgap: 14px;
    }
  }
  @media (max-width: 760px) {
    .thead {
      display: none;
    }
    .apps {
      border-radius: var(--radius);
    }
    .apps-head {
      padding: 14px var(--gutter) 12px;
    }
  }
</style>
