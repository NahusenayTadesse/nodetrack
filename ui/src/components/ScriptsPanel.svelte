<script lang="ts">
  import { installCommand, runCommand, startJob, type Control, type Job, type Script } from '../lib/api';
  import Spinner from './Spinner.svelte';

  interface Props {
    appId: number;
    packageManager: string;
    scripts: Script[];
    control: Control;
    /** the job currently running for this app, if any */
    running: Job | undefined;
    onstarted: (job: Job) => void;
  }

  let { appId, packageManager, scripts, control, running, onstarted }: Props = $props();

  const INSTALL = '\u0000install'; // row key that can't collide with a script name

  let starting = $state<string | null>(null);
  let errors = $state<Record<string, string>>({});

  const pm = $derived(packageManager || 'npm');
  const blocked = $derived.by(() => {
    if (!control.jobs) {
      return control.reason || (control.enabled ? 'Running jobs is disabled on this server.' : 'Controls are disabled on this server.');
    }
    if (running) return `Wait for the running job to finish (${running.command}).`;
    return '';
  });

  async function start(key: string) {
    starting = key;
    const { [key]: _, ...rest } = errors;
    errors = rest;
    try {
      const job = await startJob(appId, key === INSTALL ? { kind: 'install' } : { kind: 'run', script: key });
      onstarted(job);
    } catch (e) {
      errors = { ...errors, [key]: e instanceof Error ? e.message : String(e) };
    } finally {
      starting = null;
    }
  }
</script>

<section class="block" aria-labelledby="h-scripts">
  <div class="block-head">
    <h2 id="h-scripts">Scripts</h2>
    <span class="pm mono" title="Package manager">{pm}</span>
  </div>

  {#if blocked}
    <p class="blocked">
      <svg viewBox="0 0 16 16" aria-hidden="true"><circle cx="8" cy="8" r="5.5" /><path d="M8 5.2v3.3M8 10.6v.2" /></svg>
      {blocked}
    </p>
  {/if}

  <ul class="rows">
    {@render row(INSTALL, 'Install dependencies', installCommand(pm), 'Install')}
    {#each scripts as s (s.name)}
      {@render row(s.name, s.name, s.command, 'Run', runCommand(pm, s.name))}
    {/each}
  </ul>
  {#if !scripts.length}
    <p class="none">No scripts in package.json.</p>
  {/if}
</section>

{#snippet row(key: string, title: string, command: string, verb: string, runs?: string)}
  <li class="row" class:install={key === INSTALL}>
    <div class="text">
      <span class="name" class:mono={key !== INSTALL}>{title}</span>
      <code class="cmd" title={runs ? `${runs} → ${command}` : command}>{command}</code>
    </div>
    <button
      type="button"
      class="btn run"
      disabled={!!blocked || starting !== null}
      onclick={() => start(key)}
      title={blocked || (runs ? `Runs: ${runs}` : `Runs: ${command}`)}
    >
      {#if starting === key}
        <Spinner size={12} label="Starting" />
      {:else}
        <svg viewBox="0 0 16 16" aria-hidden="true"><path d="M5 3.5v9l7-4.5z" /></svg>
      {/if}
      {verb}
    </button>
    {#if errors[key]}
      <p class="err" role="alert">{errors[key]}</p>
    {/if}
  </li>
{/snippet}

<style>
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
  .pm {
    font-size: 11.5px;
    padding: 0 6px;
    border-radius: 999px;
    background: var(--surface-2);
    color: var(--muted);
  }
  .blocked {
    display: flex;
    align-items: center;
    gap: 7px;
    padding: 8px 16px;
    font-size: 12.5px;
    color: var(--muted);
    background: var(--surface-2);
    border-bottom: 1px solid var(--border);
  }
  .blocked svg {
    flex: none;
    width: 14px;
    height: 14px;
    fill: none;
    stroke: currentColor;
    stroke-width: 1.4;
    stroke-linecap: round;
  }
  .rows {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .row {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    align-items: center;
    gap: 6px 12px;
    padding: 9px 12px 9px 16px;
    border-top: 1px solid var(--border);
  }
  .row:first-child {
    border-top: 0;
  }
  .row.install {
    background: color-mix(in srgb, var(--accent) 4%, transparent);
  }
  .text {
    display: flex;
    flex-direction: column;
    gap: 1px;
    min-width: 0;
  }
  .name {
    font-size: 13px;
    font-weight: 600;
  }
  .cmd {
    font-size: 12px;
    color: var(--muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .run {
    height: 28px;
    padding: 0 10px;
    font-size: 12.5px;
  }
  .run svg {
    width: 11px;
    height: 11px;
    fill: currentColor;
  }
  .run:disabled {
    cursor: not-allowed;
  }
  .err {
    grid-column: 1 / -1;
    font-size: 12.5px;
    color: var(--down);
    overflow-wrap: anywhere;
  }
  .none {
    padding: 0 16px 12px;
    font-size: 12.5px;
    color: var(--muted);
  }
</style>
