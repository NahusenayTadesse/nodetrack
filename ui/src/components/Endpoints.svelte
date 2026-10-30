<script lang="ts">
  import { domainUrl, isLoopback, isWildcard, type Domain, type Port } from '../lib/api';

  let {
    domains,
    ports,
    sockets,
    limit = Infinity
  }: { domains: Domain[]; ports: Port[]; sockets: string[]; limit?: number } = $props();

  const portLabel = (p: Port) => (isWildcard(p.addr) ? `*:${p.port}` : isLoopback(p.addr) ? `:${p.port}` : `${p.addr}:${p.port}`);
  const portTitle = (p: Port) =>
    isWildcard(p.addr)
      ? `Listening on all interfaces, port ${p.port} (reachable from outside unless firewalled)`
      : `Listening on ${p.addr || '*'}:${p.port}`;
  // the same port on 127.0.0.1 and ::1 renders identically; show it once
  const uniquePorts = $derived(ports.filter((p, i) => ports.findIndex((q) => portLabel(q) === portLabel(p)) === i));
  const shownDomains = $derived(domains.slice(0, limit));
  const hidden = $derived(Math.max(0, domains.length - shownDomains.length));
  const domainTitle = (d: Domain) => `${domainUrl(d)} (via ${d.source})`;
</script>

{#each shownDomains as d (d.host + d.path)}
  <a class="chip domain" href={domainUrl(d)} target="_blank" rel="noopener noreferrer" title={domainTitle(d)}>
    {#if d.tls}
      <svg class="ico" viewBox="0 0 12 12" aria-label="TLS"
        ><rect x="2.5" y="5.5" width="7" height="5" rx="1" /><path d="M4 5.5V4a2 2 0 0 1 4 0v1.5" /></svg
      >
    {/if}
    <span class="host">{d.host}{d.path && d.path !== '/' ? d.path : ''}</span>
  </a>
{/each}
{#if hidden > 0}
  <span class="chip more" title={domains.slice(limit).map((d) => d.host).join(', ')}>+{hidden}</span>
{/if}
{#each uniquePorts as p (p.addr + ':' + p.port)}
  <span class="chip port" class:public={isWildcard(p.addr)} title={portTitle(p)}>{portLabel(p)}</span>
{/each}
{#each sockets as s (s)}
  <span class="chip port sock" title="Unix socket {s}">{s.split('/').pop()}</span>
{/each}
{#if !domains.length && !ports.length && !sockets.length}
  <span class="none">no listeners</span>
{/if}

<style>
  .chip {
    position: relative;
    z-index: 1;
    display: inline-flex;
    align-items: center;
    gap: 4px;
    max-width: 100%;
    height: 20px;
    padding: 0 6px;
    border-radius: 4px;
    font-family: var(--font-mono);
    font-size: 11.5px;
    white-space: nowrap;
    min-width: 0;
  }
  .host {
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .domain {
    color: var(--text-2);
    background: var(--surface-2);
    border: 1px solid var(--border);
  }
  .domain:hover {
    color: var(--accent);
    border-color: color-mix(in srgb, var(--accent) 45%, var(--border));
  }
  .ico {
    flex: none;
    width: 11px;
    height: 11px;
    fill: none;
    stroke: var(--accent);
    stroke-width: 1.3;
  }
  .port {
    color: var(--muted);
    border: 1px dashed var(--border-strong);
  }
  .port.public {
    color: var(--warn);
    border-color: color-mix(in srgb, var(--warn) 50%, transparent);
  }
  .sock {
    max-width: 140px;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .more {
    color: var(--muted);
    background: var(--surface-2);
  }
  .none {
    font-size: 12px;
    color: var(--faint);
  }
</style>
