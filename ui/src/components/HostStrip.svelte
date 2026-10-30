<script lang="ts">
  import type { Host, Tracker } from '../lib/api';
  import { bytes, bytesParts, duration, pct } from '../lib/format';
  import Meter from './Meter.svelte';

  let { host, tracker }: { host: Host; tracker: Tracker } = $props();

  const memUsed = $derived(Math.max(0, host.memTotal - host.memAvailable));
  const memPct = $derived(host.memTotal > 0 ? (memUsed / host.memTotal) * 100 : 0);
  const trackerParts = $derived(bytesParts(tracker.rss));
  const loadLevel = (l: number) => (host.cpus > 0 ? (l / host.cpus >= 1 ? 'crit' : l / host.cpus >= 0.7 ? 'warn' : '') : '');
</script>

<section class="strip" aria-label="Host">
  <div class="cell id">
    <span class="label">host</span>
    <h1 class="hostname mono" title={host.hostname}>{host.hostname}</h1>
    <p class="sub num">
      <span>{host.cpus} {host.cpus === 1 ? 'cpu' : 'cpus'}</span>
      <span class="sep">/</span>
      <span title="Host uptime">up {duration(host.uptime)}</span>
    </p>
  </div>

  <div class="cell">
    <span class="label">cpu</span>
    <p class="big num">{pct(host.cpuPct)}</p>
    <Meter value={host.cpuPct} label="Host CPU usage" />
    <p class="sub num">{((host.cpuPct / 100) * host.cpus).toFixed(1)} of {host.cpus} cores busy</p>
  </div>

  <div class="cell">
    <span class="label">memory</span>
    <p class="big num">{bytes(memUsed)}</p>
    <Meter value={memPct} label="Host memory usage" />
    <p class="sub num">{Math.round(memPct)}% of {bytes(host.memTotal)}</p>
  </div>

  <div class="cell">
    <span class="label">load</span>
    <p class="big num load">
      {#each host.load as l, i (i)}
        <span class={loadLevel(l)}>{l.toFixed(2)}</span>
      {/each}
    </p>
    <p class="sub num">1 · 5 · 15 min avg</p>
  </div>

  <div class="cell self" title="Resident memory of the nodetrack process itself">
    <span class="label">this monitor</span>
    <p class="big num">{trackerParts[0]}<span class="unit">{trackerParts[1]}</span></p>
    <p class="sub num">
      <span>nodetrack {tracker.version.startsWith('v') ? tracker.version : 'v' + tracker.version}</span>
      <span class="sep">/</span>
      <span>every {tracker.interval}s</span>
      {#if tracker.controls}
        <span class="sep">/</span>
        <span class="ctl" title="Start/stop, jobs and domain changes are enabled on this server">controls on</span>
      {/if}
    </p>
  </div>
</section>

<style>
  .strip {
    display: grid;
    grid-template-columns: minmax(0, 1.35fr) repeat(3, minmax(0, 1fr)) minmax(0, 1.1fr);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--surface);
    box-shadow: var(--shadow);
    overflow: hidden;
  }
  .cell {
    display: flex;
    flex-direction: column;
    gap: 6px;
    min-width: 0;
    padding: 14px 16px 15px;
    border-left: 1px solid var(--border);
  }
  .cell:first-child {
    border-left: 0;
  }
  .hostname {
    font-size: 17px;
    font-weight: 600;
    letter-spacing: -0.01em;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    line-height: 1.3;
  }
  .big {
    font-size: 19px;
    font-weight: 500;
    line-height: 1.25;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .unit {
    margin-left: 4px;
    font-size: 13px;
    color: var(--muted);
  }
  .sub {
    display: flex;
    gap: 6px;
    flex-wrap: wrap;
    font-size: 12px;
    color: var(--muted);
  }
  .sep {
    color: var(--faint);
  }
  .load {
    display: flex;
    gap: 10px;
  }
  .load span:not(:first-child) {
    color: var(--muted);
  }
  .load .warn {
    color: var(--warn) !important;
  }
  .load .crit {
    color: var(--down) !important;
  }
  .self {
    background:
      linear-gradient(180deg, transparent, color-mix(in srgb, var(--accent) 7%, transparent)),
      var(--surface);
  }
  .self .big {
    color: var(--accent);
    font-weight: 600;
  }
  .self .label {
    color: var(--accent);
  }
  .ctl {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    color: var(--warn);
  }
  .ctl::before {
    content: '';
    width: 5px;
    height: 5px;
    border-radius: 50%;
    background: currentColor;
  }

  @media (max-width: 960px) {
    .strip {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
    .cell {
      border-left: 0;
      border-top: 1px solid var(--border);
    }
    .id {
      grid-column: 1 / -1;
      border-top: 0;
    }
    .cell:nth-child(3),
    .cell:nth-child(5) {
      border-left: 1px solid var(--border);
    }
  }
  @media (max-width: 480px) {
    .cell {
      padding: 12px 14px 13px;
    }
    .big {
      font-size: 17px;
    }
    .load {
      gap: 7px;
      font-size: 15px;
      line-height: 1.45;
    }
  }
</style>
