<script lang="ts">
  import {
    ApiError,
    getApp,
    getHistory,
    memKind,
    memOf,
    RANGES,
    type AppDetail,
    type AppSummary,
    type History,
    type Job,
    type Range
  } from '../lib/api';
  import { bytes, coreList, duration, pct, plural, rate, relative } from '../lib/format';
  import { live } from '../lib/live.svelte';
  import { env } from '../lib/env.svelte';
  import { homeHref, router } from '../lib/router.svelte';
  import { Poll } from '../lib/poll.svelte';
  import StatusDot from '../components/StatusDot.svelte';
  import RuntimeBadge from '../components/RuntimeBadge.svelte';
  import Endpoints from '../components/Endpoints.svelte';
  import CoreMap from '../components/CoreMap.svelte';
  import Chart from '../components/Chart.svelte';
  import ProcessTable from '../components/ProcessTable.svelte';
  import EventTimeline from '../components/EventTimeline.svelte';
  import ForgetApp from '../components/ForgetApp.svelte';
  import LocationPanel from '../components/LocationPanel.svelte';
  import AppControls from '../components/AppControls.svelte';
  import DomainPicker from '../components/DomainPicker.svelte';
  import ScriptsPanel from '../components/ScriptsPanel.svelte';
  import JobConsole from '../components/JobConsole.svelte';
  import JobsList from '../components/JobsList.svelte';

  let { id }: { id: number } = $props();

  const DETAIL_REFRESH_MS = 5000;
  const HISTORY_REFRESH_MS: Record<Range, number> = {
    '1h': 10_000,
    '6h': 30_000,
    '24h': 60_000,
    '7d': 300_000,
    '30d': 600_000
  };
  const RANGE_KEY = 'nodetrack.range';

  function loadRange(): Range {
    try {
      const v = localStorage.getItem(RANGE_KEY);
      if (v && (RANGES as string[]).includes(v)) return v as Range;
    } catch {
      /* storage unavailable */
    }
    return '1h';
  }

  let range = $state<Range>(loadRange());

  // Detail: fetch now, then every 5s while this page is open; stop on 404.
  const detailPoll = new Poll<AppDetail>({
    source: () => {
      const appId = id;
      return (signal) => getApp(appId, signal);
    },
    every: () => DETAIL_REFRESH_MS,
    stopOn: (e) => e instanceof ApiError && e.status === 404
  });

  // History: refetch on range change, then at a range-appropriate pace.
  const historyPoll = new Poll<History>({
    source: () => {
      const appId = id;
      const r = range;
      return (signal) => getHistory(appId, r, signal);
    },
    every: () => HISTORY_REFRESH_MS[range]
  });

  const errMsg = (e: unknown) => (e == null ? null : e instanceof Error ? e.message : String(e));

  const detail = $derived(detailPoll.value ?? null);
  const notFound = $derived(detailPoll.stopped);
  const detailError = $derived(notFound ? null : errMsg(detailPoll.error));
  const historyError = $derived(errMsg(historyPoll.error));
  const historyLoading = $derived(historyPoll.pending);

  // Instant header from the live overview while the detail request is in flight.
  const summary = $derived<AppSummary | undefined>(live.data?.apps.find((a) => a.id === id));
  const app = $derived<AppSummary | AppDetail | undefined>(notFound ? undefined : (detail ?? summary));
  const cpus = $derived(live.data?.host.cpus ?? 0);
  const down = $derived(app?.status === 'down');

  // Job console: seeded with a Job (just started, or picked from the recent list).
  let consoleJob = $state<Job | null>(null);
  /** id of a job we started that the detail poll may not list yet */
  let pendingRunId = $state<string | null>(null);

  const runningJob = $derived(
    detail?.jobs.find((j) => j.status === 'running') ??
      (pendingRunId && consoleJob?.id === pendingRunId ? consoleJob : undefined)
  );

  function onJobStarted(job: Job) {
    consoleJob = job;
    pendingRunId = job.id;
    detailPoll.refresh();
  }

  function onJobFinished(job: Job) {
    if (pendingRunId === job.id) pendingRunId = null;
    detailPoll.refresh();
  }

  function setRange(r: Range) {
    range = r;
    try {
      localStorage.setItem(RANGE_KEY, r);
    } catch {
      /* ignore */
    }
  }

  // Keep showing the previous range's data (dimmed) while a new range loads.
  const hist = $derived<History>(historyPoll.value ?? { range, step: 0, ts: [], cpu: [], cpuMax: [], mem: [], memMax: [] });

  const sourceLabel = $derived(
    app ? (app.source === 'manual' ? 'manual' : app.unit ? `${app.source} · ${app.unit}` : app.source) : ''
  );
</script>

<svelte:head>
  <title>{app ? `${app.name} · nodetrack` : 'nodetrack'}</title>
</svelte:head>

<div class="page">
  <a class="back" href={homeHref}>
    <svg viewBox="0 0 16 16" aria-hidden="true"><path d="M10 3 5 8l5 5" /></svg>
    All apps
  </a>

  {#if notFound}
    <div class="state">
      <h1>App not found</h1>
      <p class="muted">There is no app with id {id}. It may have been forgotten.</p>
      <a class="btn" href={homeHref}>Back to overview</a>
    </div>
  {:else if !app}
    <div class="state" aria-busy="true">
      {#if detailError}
        <h1>Couldn't load this app</h1>
        <p class="muted mono">{detailError}</p>
      {:else}
        <div class="loader" aria-hidden="true"></div>
        <p class="muted">Loading…</p>
      {/if}
    </div>
  {:else}
    <header class="hero" class:down>
      <div class="top">
      <div class="ident">
      <div class="titlebar">
        <StatusDot status={app.status} />
        <h1>{app.name}</h1>
        <RuntimeBadge runtime={app.runtime} />
        {#if app.pkgVersion}<span class="ver mono">v{app.pkgVersion.replace(/^v/, '')}</span>{/if}
      </div>
      <p class="subtitle">
        <span class="mono">{sourceLabel}</span>
        <span class="faint">·</span>
        {#if down}
          <span class="downtxt">down, last seen {relative(app.lastSeen, env.now)}</span>
        {:else}
          <span>up {app.startedAt ? duration(env.now - app.startedAt) : '–'}</span>
        {/if}
        {#if app.restarts > 0}
          <span class="faint">·</span>
          <span class="restarts">{plural(app.restarts, 'restart')} in 24h</span>
        {/if}
        {#if detailError}
          <span class="faint">·</span>
          <span class="stale" title={detailError}>details stale</span>
        {/if}
      </p>
      </div>
      {#if detail}
        <div class="ctrl">
          <AppControls appId={app.id} appName={app.name} control={detail.control} onchanged={() => detailPoll.refresh()} />
        </div>
      {/if}
      </div>
      <div class="endpoints">
        <Endpoints domains={app.domains} ports={app.ports} sockets={app.sockets} />
        {#if detail?.control.enabled}
          <DomainPicker appId={app.id} appName={app.name} ports={app.ports} onchanged={() => detailPoll.refresh()} />
        {/if}
      </div>

      <dl class="stats">
        <div class="stat">
          <dt>cpu</dt>
          <dd class="num big cpu">{down ? '–' : pct(app.cpu)}</dd>
          <dd class="hint">of one core</dd>
        </div>
        <div class="stat">
          <dt>memory <span class="faint">{memKind(app)}</span></dt>
          <dd class="num big mem">{down ? '–' : bytes(memOf(app))}</dd>
          <dd class="hint num">{down ? 'not running' : app.pss > 0 ? `rss ${bytes(app.rss)}` : 'pss not readable'}</dd>
        </div>
        <div class="stat">
          <dt>processes</dt>
          <dd class="num big">{app.procs}</dd>
          <dd class="hint num">{app.threads} threads · {app.fds} fds</dd>
        </div>
        <div class="stat">
          <dt>disk i/o</dt>
          <dd class="num io"><span class="faint">r</span> {rate(app.readBps)}</dd>
          <dd class="num io"><span class="faint">w</span> {rate(app.writeBps)}</dd>
        </div>
        <div class="stat">
          <dt>cores</dt>
          <dd class="cores"><CoreMap cores={app.cores} {cpus} dim={down} /></dd>
          <dd class="hint num">
            {app.cores.length ? `ran on ${coreList(app.cores)}` : 'idle'}{app.affinity ? ` · allowed ${app.affinity}` : ''}
          </dd>
        </div>
      </dl>
    </header>

    <LocationPanel {app} {detail} />

    <section class="charts-section" aria-labelledby="h-hist">
      <div class="section-head">
        <h2 id="h-hist">History</h2>
        <div class="seg" role="radiogroup" aria-label="Time range">
          {#each RANGES as r (r)}
            <button type="button" role="radio" aria-checked={range === r} onclick={() => setRange(r)}>{r}</button>
          {/each}
        </div>
      </div>
      {#if historyError}
        <p class="hist-err" role="alert">Couldn't load history: {historyError}</p>
      {/if}
      <div class="charts">
        <Chart
          kind="cpu"
          title="CPU"
          ts={hist.ts}
          avg={hist.cpu}
          peak={hist.cpuMax}
          loading={historyLoading}
        />
        <Chart
          kind="mem"
          title="Memory"
          ts={hist.ts}
          avg={hist.mem}
          peak={hist.memMax}
          loading={historyLoading}
        />
      </div>
    </section>

    {#if detail}
      <div class="ops">
        <ScriptsPanel
          appId={app.id}
          packageManager={detail.location?.packageManager ?? ''}
          scripts={detail.scripts}
          control={detail.control}
          running={runningJob}
          onstarted={onJobStarted}
        />
        <div class="jobs-col">
          {#if consoleJob}
            {#key consoleJob.id}
              <JobConsole job={consoleJob} onclose={() => (consoleJob = null)} onfinished={onJobFinished} />
            {/key}
          {/if}
          <section class="block" aria-labelledby="h-jobs">
            <div class="block-head">
              <h2 id="h-jobs">Recent jobs</h2>
              {#if detail.jobs.length}<span class="count num">{detail.jobs.length}</span>{/if}
            </div>
            <JobsList jobs={detail.jobs} activeId={consoleJob?.id ?? null} onselect={(j) => (consoleJob = j)} />
          </section>
        </div>
      </div>
    {/if}

    <div class="split">
      <section class="block" aria-labelledby="h-procs">
        <div class="block-head">
          <h2 id="h-procs">Processes</h2>
          {#if detail}<span class="count num">{detail.processes.length}</span>{/if}
        </div>
        {#if detail}
          <ProcessTable processes={detail.processes} />
        {:else}
          <p class="pending">Loading…</p>
        {/if}
      </section>

      <section class="block" aria-labelledby="h-events">
        <div class="block-head">
          <h2 id="h-events">Events</h2>
        </div>
        {#if detail}
          <EventTimeline events={detail.events} />
        {:else}
          <p class="pending">Loading…</p>
        {/if}
      </section>
    </div>

    {#if down}
      <ForgetApp id={app.id} name={app.name} onforgotten={() => router.go('/')} />
    {/if}
  {/if}
</div>

<style>
  .page {
    display: flex;
    flex-direction: column;
    gap: 18px;
  }
  .back {
    align-self: flex-start;
    display: inline-flex;
    align-items: center;
    gap: 4px;
    margin-bottom: -4px;
    font-size: 13px;
    color: var(--muted);
  }
  .back:hover {
    color: var(--text);
  }
  .back svg {
    width: 14px;
    height: 14px;
    fill: none;
    stroke: currentColor;
    stroke-width: 1.8;
    stroke-linecap: round;
    stroke-linejoin: round;
  }

  .hero {
    display: flex;
    flex-direction: column;
    gap: 10px;
    padding: 20px 20px 0;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--surface);
    box-shadow: var(--shadow);
    overflow: hidden;
  }
  .top {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 12px 20px;
    flex-wrap: wrap;
  }
  .ident {
    display: flex;
    flex-direction: column;
    gap: 10px;
    min-width: 0;
    flex: 1 1 320px;
  }
  .ctrl {
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    gap: 8px;
    min-width: 0;
    max-width: 100%;
    flex: 0 1 auto;
  }
  .ops {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, 1.5fr);
    gap: 14px;
    align-items: start;
  }
  .jobs-col {
    display: flex;
    flex-direction: column;
    gap: 14px;
    min-width: 0;
  }
  .titlebar {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
    min-width: 0;
  }
  h1 {
    font-size: 24px;
    font-weight: 650;
    letter-spacing: -0.02em;
    line-height: 1.2;
    overflow-wrap: anywhere;
  }
  .ver {
    font-size: 12px;
    color: var(--muted);
  }
  .subtitle {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    font-size: 13px;
    color: var(--text-2);
  }
  .downtxt,
  .stale {
    color: var(--down);
  }
  .restarts {
    color: var(--warn);
    font-weight: 600;
  }
  .endpoints {
    display: flex;
    flex-wrap: wrap;
    gap: 5px;
  }
  .stats {
    display: grid;
    grid-template-columns: repeat(5, minmax(0, 1fr));
    margin: 8px -20px 0;
    border-top: 1px solid var(--border);
  }
  .stat {
    display: flex;
    flex-direction: column;
    gap: 3px;
    padding: 14px 20px 16px;
    border-left: 1px solid var(--border);
    min-width: 0;
  }
  .stat:first-child {
    border-left: 0;
  }
  dt {
    font-size: 11px;
    font-weight: 600;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--muted);
  }
  dd {
    margin: 0;
    min-width: 0;
  }
  .big {
    font-size: 22px;
    line-height: 1.25;
  }
  .big.cpu {
    color: var(--cpu);
  }
  .big.mem {
    color: var(--mem);
  }
  .down .big {
    color: var(--faint);
  }
  .io {
    font-size: 13.5px;
    line-height: 1.5;
  }
  .cores {
    padding: 5px 0 3px;
  }
  .hint {
    font-size: 12px;
    color: var(--muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .block {
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--surface);
    box-shadow: var(--shadow);
    overflow: hidden;
    min-width: 0;
  }
  .block-head {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 12px 16px;
    border-bottom: 1px solid var(--border);
  }
  h2 {
    font-size: 14px;
    font-weight: 600;
  }
  .count {
    font-size: 11.5px;
    padding: 0 6px;
    border-radius: 999px;
    background: var(--surface-2);
    color: var(--muted);
  }
  .pending {
    padding: 16px;
    color: var(--muted);
    font-size: 13px;
  }

  .charts-section {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .section-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    flex-wrap: wrap;
  }
  .charts {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 14px;
  }
  .hist-err {
    font-size: 13px;
    color: var(--down);
  }

  .split {
    display: grid;
    grid-template-columns: minmax(0, 1.6fr) minmax(0, 1fr);
    gap: 14px;
    align-items: start;
  }

  .state {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 10px;
    padding: 80px 16px;
    text-align: center;
  }
  .state h1 {
    font-size: 18px;
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
    .charts,
    .ops,
    .split {
      grid-template-columns: minmax(0, 1fr);
    }
    .stats {
      grid-template-columns: repeat(3, minmax(0, 1fr));
    }
    .stat {
      border-top: 1px solid var(--border);
    }
    .stat:nth-child(-n + 3) {
      border-top: 0;
    }
    .stat:nth-child(4) {
      border-left: 0;
    }
  }
  @media (max-width: 640px) {
    .hero {
      padding: 16px 16px 0;
    }
    h1 {
      font-size: 21px;
    }
    .ctrl {
      align-items: stretch;
      flex-basis: 100%;
    }
    .stats {
      grid-template-columns: repeat(2, minmax(0, 1fr));
      margin: 6px -16px 0;
    }
    .stat {
      padding: 12px 16px 14px;
      border-left: 0;
      border-top: 1px solid var(--border);
    }
    .stat:nth-child(-n + 3) {
      border-top: 1px solid var(--border);
    }
    .stat:nth-child(-n + 2) {
      border-top: 0;
    }
    .stat:nth-child(even) {
      border-left: 1px solid var(--border);
    }
    .stat:last-child {
      grid-column: 1 / -1;
    }
    .big {
      font-size: 19px;
    }
  }
</style>
