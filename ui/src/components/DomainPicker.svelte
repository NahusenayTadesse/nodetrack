<script lang="ts">
  import { getDomains, pointDomain, tcpPorts, type DomainRoute, type Port } from '../lib/api';
  import { duration } from '../lib/format';
  import { env } from '../lib/env.svelte';
  import Spinner from './Spinner.svelte';

  interface Props {
    appId: number;
    appName: string;
    ports: Port[];
    /** called after a domain was pointed here so the page can refetch */
    onchanged: () => void;
  }

  let { appId, appName, ports, onchanged }: Props = $props();

  let open = $state(false);
  let routes = $state<DomainRoute[] | null>(null);
  let loadError = $state<string | null>(null);
  let loading = $state(false);
  let filter = $state('');
  let pickedPort = $state<number | null>(null);
  let selected = $state<DomainRoute | null>(null);
  let busy = $state<{ since: number; route: DomainRoute; port: number } | null>(null);
  let result = $state<{ ok: boolean; message: string; output: string } | null>(null);

  const tcp = $derived(tcpPorts(ports));
  const port = $derived(pickedPort !== null && tcp.includes(pickedPort) ? pickedPort : tcp[0]);

  const candidates = $derived.by(() => {
    const q = filter.trim().toLowerCase();
    return (routes ?? [])
      .filter((r) => r.appId !== appId)
      .filter((r) => !q || r.hosts.some((h) => h.toLowerCase().includes(q)) || r.path.toLowerCase().includes(q))
      .sort(
        (a, b) =>
          Number(b.editable) - Number(a.editable) ||
          a.source.localeCompare(b.source) ||
          (a.hosts[0] ?? '').localeCompare(b.hosts[0] ?? '') ||
          a.path.localeCompare(b.path)
      );
  });

  async function load() {
    loading = true;
    loadError = null;
    try {
      routes = await getDomains();
    } catch (e) {
      loadError = e instanceof Error ? e.message : String(e);
    } finally {
      loading = false;
    }
  }

  function toggle() {
    open = !open;
    selected = null;
    if (open) {
      result = null;
      load();
    }
  }

  const hostLabel = (r: DomainRoute) => `${r.hosts[0] ?? '(no host)'}${r.path && r.path !== '/' ? r.path : ''}`;

  function target(r: DomainRoute): string {
    if (r.static) return 'static site';
    if (r.appId != null) return `→ ${r.appName || `app ${r.appId}`}${r.port ? ` :${r.port}` : ''}`;
    if (r.port > 0) return `→ :${r.port}`;
    if (r.socket) return `→ ${r.socket.split('/').pop()}`;
    return 'unassigned';
  }

  async function apply(r: DomainRoute, p: number) {
    selected = null;
    result = null;
    busy = { since: Math.floor(Date.now() / 1000), route: r, port: p };
    try {
      const res = await pointDomain(appId, r.id, p);
      result = {
        ok: true,
        message: `${hostLabel(r)} now points at ${appName} on :${p}. The domain list updates within ~15s.`,
        output: res?.output ?? ''
      };
      onchanged();
      load();
    } catch (e) {
      result = { ok: false, message: `Couldn't point ${hostLabel(r)} here:`, output: e instanceof Error ? e.message : String(e) };
    } finally {
      busy = null;
    }
  }

  // Escape backs out of the confirm step, then closes the panel.
  const escape = (node: HTMLElement) => {
    const onkeydown = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return;
      if (selected) selected = null;
      else if (!busy) open = false;
    };
    node.addEventListener('keydown', onkeydown);
    return () => node.removeEventListener('keydown', onkeydown);
  };

  const focusOnMount = (node: HTMLElement) => node.focus();
</script>

<button type="button" class="trigger" class:on={open} aria-expanded={open} onclick={toggle}>
  <svg viewBox="0 0 16 16" aria-hidden="true"><path d="M8 3.5v9M3.5 8h9" /></svg>
  Point a domain here…
</button>

{#if open}
  <div class="panel" role="region" aria-label="Point a domain at {appName}" {@attach escape}>
    <div class="head">
      <div>
        <h3>Point a domain at <span class="mono">{appName}</span></h3>
        <p class="sub">Routes known to the reverse proxies on this server.</p>
      </div>
      <button type="button" class="x" aria-label="Close" onclick={toggle} disabled={!!busy}>×</button>
    </div>

    {#if !tcp.length}
      <p class="note">This app doesn't listen on a TCP port, so there's nothing to point a domain at yet.</p>
    {:else}
      {#if tcp.length > 1}
        <div class="portpick">
          <span class="label">serve on</span>
          <div class="seg" role="radiogroup" aria-label="Target port">
            {#each tcp as p (p)}
              <button
                type="button"
                role="radio"
                aria-checked={port === p}
                disabled={!!busy || !!selected}
                onclick={() => (pickedPort = p)}>:{p}</button
              >
            {/each}
          </div>
        </div>
      {/if}

      {#if busy}
        <div class="progress" role="status">
          <Spinner label="Applying" />
          <div>
            <p>
              Pointing <strong class="mono">{hostLabel(busy.route)}</strong> at :{busy.port} and waiting for
              {busy.route.source} to restart gracefully…
            </p>
            <p class="sub num">{duration(Math.max(0, env.now - busy.since))} · can take up to 60s</p>
          </div>
        </div>
      {:else if selected}
        {@const s = selected}
        <div class="confirm">
          <p>
            {#if s.static}
              <strong class="mono">{hostLabel(s)}</strong> serves static files today. A proxy to
              <strong>{appName}</strong> on :{port} will be put in front of it, so
              <em>the site's files will be hidden behind this app</em>.
            {:else if s.appId != null}
              <strong class="mono">{hostLabel(s)}</strong> will stop serving
              <strong>{s.appName || `app ${s.appId}`}</strong> and serve <strong>{appName}</strong> on :{port} instead.
            {:else if s.port > 0 || s.socket}
              <strong class="mono">{hostLabel(s)}</strong> currently proxies to
              <span class="mono">{s.port > 0 ? `:${s.port}` : s.socket}</span>. It will serve
              <strong>{appName}</strong> on :{port} instead.
            {:else}
              <strong class="mono">{hostLabel(s)}</strong> will serve <strong>{appName}</strong> on :{port}.
            {/if}
            {#if s.hosts.length > 1}
              <span class="sub">Also affects {s.hosts.slice(1).join(', ')}.</span>
            {/if}
            <span class="sub">{s.source} will be reloaded gracefully.</span>
          </p>
          <div class="actions">
            <button type="button" class="btn" onclick={() => (selected = null)}>Cancel</button>
            <button type="button" class="btn primary" {@attach focusOnMount} onclick={() => apply(s, port)}
              >Point it here</button
            >
          </div>
        </div>
      {/if}

      {#if result}
        <div class="result" class:ok={result.ok} role={result.ok ? 'status' : 'alert'}>
          <p>{result.message}</p>
          {#if !result.ok}
            <pre>{result.output}</pre>
          {:else if result.output.trim()}
            <details>
              <summary>Output</summary>
              <pre>{result.output.trim()}</pre>
            </details>
          {/if}
        </div>
      {/if}

      {#if !busy && !selected}
        {#if (routes?.length ?? 0) > 6}
          <input class="filter" type="search" placeholder="Filter domains" bind:value={filter} aria-label="Filter domains" />
        {/if}
        {#if loading && !routes}
          <p class="note"><Spinner size={12} label="Loading" /> Loading domains…</p>
        {:else if loadError}
          <p class="note err" role="alert">Couldn't load domains: {loadError}</p>
        {:else if !candidates.length}
          <p class="note">
            {filter ? 'No domains match.' : 'No other domain routes found in the nginx / Caddy / OpenLiteSpeed configs.'}
          </p>
        {:else}
          <ul class="list">
            {#each candidates as r (r.id)}
              <li>
                <button
                  type="button"
                  class="route"
                  disabled={!r.editable}
                  onclick={() => {
                    result = null;
                    selected = r;
                  }}
                >
                  <span class="host">
                    {#if r.tls}
                      <svg class="lock" viewBox="0 0 12 12" aria-label="TLS"
                        ><rect x="2.5" y="5.5" width="7" height="5" rx="1" /><path d="M4 5.5V4a2 2 0 0 1 4 0v1.5" /></svg
                      >
                    {/if}
                    <span class="mono h">{hostLabel(r)}</span>
                    {#if r.hosts.length > 1}<span class="more" title={r.hosts.slice(1).join(', ')}>+{r.hosts.length - 1}</span>{/if}
                  </span>
                  <span class="meta">
                    <span class="tgt mono" class:static={r.static} class:free={!r.static && r.appId == null && !r.port && !r.socket}
                      >{target(r)}</span
                    >
                    <span class="src mono">{r.source}</span>
                  </span>
                  {#if !r.editable && r.reason}
                    <span class="reason">{r.reason}</span>
                  {/if}
                </button>
              </li>
            {/each}
          </ul>
        {/if}
      {/if}
    {/if}
  </div>
{/if}

<style>
  .trigger {
    position: relative;
    z-index: 1;
    display: inline-flex;
    align-items: center;
    gap: 4px;
    height: 20px;
    padding: 0 7px 0 5px;
    border-radius: 4px;
    border: 1px dashed var(--border-strong);
    background: transparent;
    color: var(--muted);
    font-size: 11.5px;
    cursor: pointer;
  }
  .trigger:hover,
  .trigger.on {
    color: var(--accent);
    border-color: color-mix(in srgb, var(--accent) 50%, var(--border));
  }
  .trigger svg {
    width: 11px;
    height: 11px;
    fill: none;
    stroke: currentColor;
    stroke-width: 1.6;
    stroke-linecap: round;
  }
  .panel {
    flex-basis: 100%;
    display: flex;
    flex-direction: column;
    gap: 10px;
    margin-top: 4px;
    padding: 12px;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--bg);
    min-width: 0;
  }
  .head {
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
    gap: 8px;
  }
  h3 {
    font-size: 13.5px;
    font-weight: 600;
  }
  .sub {
    display: block;
    font-size: 12px;
    color: var(--muted);
  }
  .x {
    flex: none;
    width: 26px;
    height: 26px;
    border: 0;
    border-radius: 5px;
    background: transparent;
    color: var(--muted);
    font-size: 18px;
    line-height: 1;
    cursor: pointer;
  }
  .x:hover {
    background: var(--surface-2);
    color: var(--text);
  }
  .portpick {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
  }
  .portpick .seg button {
    font-family: var(--font-mono);
  }
  .note {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 12.5px;
    color: var(--muted);
  }
  .note.err {
    color: var(--down);
  }
  .filter {
    height: 32px;
    padding: 0 10px;
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-sm);
    background: var(--surface);
    color: var(--text);
    font: inherit;
    font-size: 13px;
  }
  .list {
    list-style: none;
    margin: 0;
    padding: 0;
    max-height: 340px;
    overflow: auto;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--surface);
  }
  .list li + li {
    border-top: 1px solid var(--border);
  }
  .route {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    align-items: center;
    gap: 3px 12px;
    width: 100%;
    padding: 9px 12px;
    border: 0;
    background: transparent;
    text-align: left;
    cursor: pointer;
    font-size: 12.5px;
  }
  .route:hover:not(:disabled) {
    background: var(--surface-2);
  }
  .route:disabled {
    cursor: not-allowed;
  }
  .route:disabled .host,
  .route:disabled .meta {
    opacity: 0.55;
  }
  .host {
    display: flex;
    align-items: center;
    gap: 5px;
    min-width: 0;
  }
  .h {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-weight: 600;
    color: var(--text);
  }
  .lock {
    flex: none;
    width: 11px;
    height: 11px;
    fill: none;
    stroke: var(--accent);
    stroke-width: 1.3;
  }
  .more {
    flex: none;
    font-size: 11px;
    color: var(--muted);
  }
  .meta {
    display: flex;
    align-items: center;
    gap: 8px;
    white-space: nowrap;
  }
  .tgt {
    font-size: 11.5px;
    color: var(--text-2);
  }
  .tgt.static {
    color: var(--info);
  }
  .tgt.free {
    color: var(--faint);
  }
  .src {
    font-size: 10.5px;
    padding: 1px 5px;
    border-radius: 4px;
    background: var(--surface-2);
    color: var(--muted);
  }
  .reason {
    grid-column: 1 / -1;
    font-size: 11.5px;
    color: var(--muted);
  }
  .confirm,
  .progress {
    display: flex;
    gap: 12px;
    padding: 12px;
    border-radius: var(--radius-sm);
    border: 1px solid color-mix(in srgb, var(--warn) 45%, var(--border));
    background: color-mix(in srgb, var(--warn) 6%, var(--surface));
    font-size: 13px;
    line-height: 1.5;
  }
  .confirm {
    flex-direction: column;
  }
  .confirm em {
    font-style: normal;
    color: var(--warn);
    font-weight: 600;
  }
  .progress {
    align-items: flex-start;
    border-color: var(--border);
    background: var(--surface);
    color: var(--accent);
  }
  .progress p:first-child {
    color: var(--text);
  }
  .actions {
    display: flex;
    gap: 8px;
    justify-content: flex-end;
    flex-wrap: wrap;
  }
  .btn.primary {
    background: var(--text);
    border-color: var(--text);
    color: var(--bg);
  }
  .btn.primary:hover {
    background: color-mix(in srgb, var(--text) 85%, var(--bg));
  }
  .result {
    padding: 10px 12px;
    border-radius: var(--radius-sm);
    border: 1px solid color-mix(in srgb, var(--down) 40%, var(--border));
    background: color-mix(in srgb, var(--down) 5%, var(--surface));
    color: var(--down);
    font-size: 13px;
  }
  .result.ok {
    border-color: color-mix(in srgb, var(--up) 40%, var(--border));
    background: color-mix(in srgb, var(--up) 5%, var(--surface));
    color: var(--text-2);
  }
  .result pre {
    margin: 8px 0 0;
    max-height: 200px;
    overflow: auto;
    font-family: var(--font-mono);
    font-size: 12px;
    white-space: pre-wrap;
    word-break: break-word;
    color: var(--text-2);
  }
  @media (max-width: 560px) {
    .route {
      grid-template-columns: minmax(0, 1fr);
    }
    .panel {
      padding: 10px;
    }
  }
  summary {
    margin-top: 6px;
    font-size: 12px;
    color: var(--muted);
    cursor: pointer;
  }
</style>
