<script lang="ts">
  import type { AppDetail, AppSummary } from '../lib/api';
  import { datetime, relative } from '../lib/format';
  import { env } from '../lib/env.svelte';
  import PathText from './PathText.svelte';
  import CopyButton from './CopyButton.svelte';

  let { app, detail }: { app: AppSummary; detail: AppDetail | null } = $props();

  const loc = $derived(detail?.location ?? null);
  const dir = $derived(loc?.dir || app.cwd);
  const version = $derived(app.pkgVersion ? `v${app.pkgVersion.replace(/^v/, '')}` : '');
  const showCwd = $derived(!!loc && !!loc.cwd && loc.cwd !== loc.dir);

  /** Browser link for a git remote: https URLs as-is, scp-style `git@host:path` converted. */
  function remoteHref(remote: string): string | undefined {
    if (/^https?:\/\//.test(remote)) return remote.replace(/\.git$/, '');
    const m = /^(?:ssh:\/\/)?git@([^:/]+)[:/](.+?)(?:\.git)?$/.exec(remote);
    return m ? `https://${m[1]}/${m[2]}` : undefined;
  }
</script>

<section class="block" aria-labelledby="h-loc">
  <div class="block-head">
    <h2 id="h-loc">Location</h2>
    {#if loc?.user}
      <span class="who mono" title="Owner of the app's files. Jobs run as this user.">
        <svg viewBox="0 0 16 16" aria-hidden="true"
          ><circle cx="8" cy="5.5" r="2.5" /><path d="M3 13.5c.6-2.4 2.6-3.8 5-3.8s4.4 1.4 5 3.8" /></svg
        >{loc.user}
      </span>
    {/if}
  </div>

  {#if loc?.logs}
    <div class="logs">
      <span class="label">logs</span>
      <code class="cmd" title={loc.logs}><span class="prompt" aria-hidden="true">$</span>{loc.logs}</code>
      <CopyButton text={loc.logs} label="Copy log command" />
    </div>
  {/if}

  <dl class="info">
    <div>
      <dt>{loc ? 'package' : 'cwd'}</dt>
      <dd>{#if dir}<PathText value={dir} />{:else}–{/if}</dd>
    </div>
    {#if showCwd && loc}
      <div>
        <dt>cwd</dt>
        <dd><PathText value={loc.cwd} /></dd>
      </div>
    {/if}
    {#if loc}
      <div>
        <dt>entry</dt>
        <dd>{#if loc.entry}<PathText value={loc.entry} />{:else}<span class="faint">unknown</span>{/if}</dd>
      </div>
    {/if}
    <div>
      <dt>command</dt>
      <dd class="row">
        <span class="mono ell" title={app.command}>{app.command || '–'}</span>
        {#if app.command}<CopyButton text={app.command} label="Copy command" />{/if}
      </dd>
    </div>
    {#if loc}
      <div>
        <dt>runtime</dt>
        <dd class="row">
          <span class="rt mono">{app.runtime}{loc.runtimeVersion ? ` ${loc.runtimeVersion}` : ''}</span>
          {#if loc.exe}<PathText value={loc.exe} />{/if}
        </dd>
      </div>
      <div>
        <dt>packages</dt>
        <dd class="mono">
          {loc.packageManager || '–'}{#if version}<span class="faint sep">·</span>{version}{/if}
        </dd>
      </div>
      {#if loc.unitFile}
        <div>
          <dt>unit file</dt>
          <dd><PathText value={loc.unitFile} /></dd>
        </div>
      {/if}
    {:else}
      <div>
        <dt>package</dt>
        <dd class="mono">{version || '–'}</dd>
      </div>
    {/if}
    <div>
      <dt>git</dt>
      <dd class="mono row">
        {#if detail?.git}
          <span class="branch">{detail.git.branch || 'detached'}</span>
          <span class="faint">@</span>
          <span class="commit">{detail.git.commit.slice(0, 12)}</span>
        {:else if detail}
          <span class="faint">not a git repo</span>
        {:else}
          <span class="faint">…</span>
        {/if}
      </dd>
    </div>
    {#if loc?.gitRemote}
      <div>
        <dt>remote</dt>
        <dd><PathText value={loc.gitRemote} href={remoteHref(loc.gitRemote)} /></dd>
      </div>
    {/if}
    <div>
      <dt>affinity</dt>
      <dd class="mono">{app.affinity || '–'}</dd>
    </div>
    <div>
      <dt>pids</dt>
      <dd class="mono ell">{app.pids.length ? app.pids.join(', ') : '–'}</dd>
    </div>
    <div>
      <dt>first seen</dt>
      <dd class="num" title={datetime(app.firstSeen)}>{relative(app.firstSeen, env.now)}</dd>
    </div>
    <div>
      <dt>started</dt>
      <dd class="num">{app.startedAt ? datetime(app.startedAt) : '–'}</dd>
    </div>
  </dl>
</section>

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
    justify-content: space-between;
    gap: 8px;
    padding: 12px 16px;
    border-bottom: 1px solid var(--border);
  }
  h2 {
    font-size: 14px;
    font-weight: 600;
  }
  .who {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    font-size: 12px;
    color: var(--muted);
  }
  .who svg {
    width: 13px;
    height: 13px;
    fill: none;
    stroke: currentColor;
    stroke-width: 1.4;
  }
  .logs {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 10px 16px;
    border-bottom: 1px solid var(--border);
    background: var(--surface-2);
    min-width: 0;
  }
  .cmd {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 12.5px;
    color: var(--text);
  }
  .prompt {
    margin-right: 8px;
    color: var(--accent);
    user-select: none;
  }
  .info {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    margin: 0;
  }
  .info > div {
    display: grid;
    grid-template-columns: 84px minmax(0, 1fr);
    gap: 12px;
    align-items: center;
    min-height: 38px;
    padding: 6px 16px;
    border-top: 1px solid var(--border);
    font-size: 13px;
  }
  .info > div:nth-child(-n + 2) {
    border-top: 0;
  }
  .info > div:nth-child(even) {
    border-left: 1px solid var(--border);
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
    color: var(--text-2);
  }
  .row {
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
  }
  .ell {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .rt {
    flex: none;
    padding: 1px 6px;
    border-radius: 4px;
    font-size: 11.5px;
    background: var(--surface-2);
    color: var(--text);
  }
  .branch {
    color: var(--accent);
  }
  .sep {
    margin: 0 6px;
  }

  @media (max-width: 640px) {
    .info {
      grid-template-columns: minmax(0, 1fr);
    }
    .info > div {
      grid-template-columns: 74px minmax(0, 1fr);
      border-left: 0 !important;
      border-top: 1px solid var(--border) !important;
    }
    .info > div:first-child {
      border-top: 0 !important;
    }
  }
</style>
