<script lang="ts">
  import { memKind, memOf, type AppSummary } from '../lib/api';
  import { bytes, duration, pct, relative } from '../lib/format';
  import { env } from '../lib/env.svelte';
  import { appHref } from '../lib/router.svelte';
  import StatusDot from './StatusDot.svelte';
  import RuntimeBadge from './RuntimeBadge.svelte';
  import Sparkline from './Sparkline.svelte';
  import CoreMap from './CoreMap.svelte';
  import Endpoints from './Endpoints.svelte';

  let { app, cpus }: { app: AppSummary; cpus: number } = $props();

  const down = $derived(app.status === 'down');
  const mem = $derived(memOf(app));
  const memMin = $derived(app.sparkMem.length ? Math.max(...app.sparkMem) * 0.08 : 1);
</script>

<article class="row" class:down aria-label="{app.name}, {app.status}">
  <div class="c-app">
    <div class="title">
      <StatusDot status={app.status} />
      <a class="name" href={appHref(app.id)}>{app.name}</a>
      <RuntimeBadge runtime={app.runtime} />
      <span class="src mono" title={app.unit ? `${app.source}: ${app.unit}` : 'Discovered from a listening port'}
        >{app.source}</span
      >
    </div>
    <div class="endpoints">
      <Endpoints domains={app.domains} ports={app.ports} sockets={app.sockets} limit={3} />
    </div>
  </div>

  <div class="c-metric c-cpu">
    <span class="mlabel label">cpu</span>
    <span class="val num" title="% of one core">{down ? '–' : pct(app.cpu)}</span>
    <div class="spark">
      <Sparkline values={app.sparkCpu} min={0} minRange={2} muted={down} label="CPU over the last samples" />
    </div>
  </div>

  <div class="c-metric c-mem">
    <span class="mlabel label">mem</span>
    <span class="val num" title={memKind(app)}>{down ? '–' : bytes(mem)}</span>
    <div class="spark">
      <Sparkline
        values={app.sparkMem}
        color="var(--mem)"
        fill="var(--mem-fill)"
        minRange={memMin}
        muted={down}
        label="Memory over the last samples"
      />
    </div>
  </div>

  <div class="c-uptime">
    <span class="mlabel label">{down ? 'last seen' : 'uptime'}</span>
    {#if down}
      <span class="num downtxt" title="Last seen {relative(app.lastSeen, env.now)}">{relative(app.lastSeen, env.now)}</span>
    {:else}
      <span class="num">{app.startedAt ? duration(env.now - app.startedAt) : '–'}</span>
    {/if}
  </div>

  <div class="c-restarts">
    <span class="mlabel label">restarts</span>
    <span class="num restarts" class:hot={app.restarts > 0} title="Restarts in the last 24h">{app.restarts}</span>
  </div>

  <div class="c-cores">
    <span class="mlabel label">cores</span>
    <CoreMap cores={app.cores} {cpus} dim={down} />
  </div>
</article>

<style>
  .row {
    position: relative;
    display: grid;
    grid-template-columns: var(--cols);
    gap: var(--colgap);
    align-items: center;
    padding: 12px 16px;
    border-top: 1px solid var(--border);
    transition: background 0.12s;
  }
  .row:hover {
    background: var(--surface-2);
  }
  .c-app {
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .title {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
  }
  .name {
    font-weight: 600;
    font-size: 14.5px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-width: 0;
  }
  /* stretched link: whole row navigates, nested chips stay clickable */
  .name::after {
    content: '';
    position: absolute;
    inset: 0;
  }
  .name:focus-visible {
    box-shadow: none;
  }
  .name:focus-visible::after {
    box-shadow: inset 0 0 0 2px var(--accent);
    border-radius: 0;
  }
  .src {
    font-size: 11px;
    color: var(--muted);
  }
  .endpoints {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
    min-width: 0;
    padding-left: 16px;
  }
  .c-metric {
    display: grid;
    grid-template-columns: 62px minmax(0, 1fr);
    align-items: center;
    gap: 10px;
    min-width: 0;
  }
  .val {
    font-size: 13px;
    text-align: right;
  }
  .spark {
    height: 26px;
    min-width: 0;
  }
  .c-uptime,
  .c-restarts,
  .c-cores {
    font-size: 13px;
    min-width: 0;
  }
  .c-restarts,
  .c-cores {
    text-align: right;
  }
  .restarts {
    color: var(--faint);
  }
  .restarts.hot {
    display: inline-block;
    min-width: 22px;
    padding: 1px 6px;
    border-radius: 4px;
    text-align: center;
    color: var(--warn);
    background: var(--warn-soft);
    font-weight: 600;
  }
  .mlabel {
    display: none;
  }
  .downtxt {
    color: var(--down);
  }

  .down .name {
    color: var(--muted);
  }
  .down .val,
  .down .c-uptime .num:not(.downtxt) {
    color: var(--faint);
  }
  .down .endpoints {
    opacity: 0.6;
  }

  @media (max-width: 760px) {
    .row {
      grid-template-columns: repeat(6, minmax(0, 1fr));
      grid-template-areas:
        'app app app app app app'
        'cpu cpu cpu mem mem mem'
        'up up re re co co';
      row-gap: 12px;
      column-gap: 16px;
      padding: 14px var(--gutter);
    }
    .c-app {
      grid-area: app;
    }
    .c-cpu {
      grid-area: cpu;
    }
    .c-mem {
      grid-area: mem;
    }
    .c-uptime {
      grid-area: up;
    }
    .c-restarts {
      grid-area: re;
    }
    .c-cores {
      grid-area: co;
    }
    .c-metric {
      grid-template-columns: auto minmax(0, 1fr);
      grid-template-rows: auto 24px;
      row-gap: 4px;
      column-gap: 8px;
    }
    .c-metric .val {
      text-align: left;
    }
    .c-metric .spark {
      grid-column: 1 / -1;
      height: 24px;
    }
    .c-uptime,
    .c-restarts,
    .c-cores {
      display: flex;
      flex-direction: column;
      gap: 2px;
      text-align: left;
      align-items: flex-start;
    }
    .mlabel {
      display: block;
      font-size: 10px;
    }
    .endpoints {
      padding-left: 0;
    }
  }
</style>
