<script lang="ts">
  import { appAction, ApiError, type Action, type Control } from '../lib/api';
  import { duration } from '../lib/format';
  import { env } from '../lib/env.svelte';
  import Spinner from './Spinner.svelte';

  interface Props {
    appId: number;
    appName: string;
    control: Control;
    /** called after an action completes so the page can refetch */
    onchanged: () => void;
  }

  let { appId, appName, control, onchanged }: Props = $props();

  const ORDER: Action[] = ['start', 'stop', 'restart'];
  const VERB: Record<Action, [label: string, doing: string, done: string]> = {
    start: ['Start', 'Starting', 'Started'],
    stop: ['Stop', 'Stopping', 'Stopped'],
    restart: ['Restart', 'Restarting', 'Restarted']
  };

  let confirming = $state<Action | null>(null);
  let busy = $state<{ action: Action; since: number } | null>(null);
  let result = $state<{ action: Action; ok: boolean; message: string; output: string } | null>(null);

  const actions = $derived(ORDER.filter((a) => control.actions.includes(a)));

  async function run(action: Action) {
    confirming = null;
    result = null;
    busy = { action, since: Math.floor(Date.now() / 1000) };
    try {
      const res = await appAction(appId, action);
      result = {
        action,
        ok: true,
        message: `${VERB[action][2]}. The status updates on the next tick.`,
        output: res?.output ?? ''
      };
      onchanged();
      // status flips on the next collection tick (≤5s); look again then
      setTimeout(onchanged, 5500);
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      const prefix = e instanceof ApiError && e.status === 403 ? 'Not allowed: ' : `${VERB[action][0]} failed: `;
      result = { action, ok: false, message: prefix, output: msg };
    } finally {
      busy = null;
    }
  }

  function ask(action: Action) {
    result = null;
    if (action === 'start') run(action);
    else confirming = action;
  }

  function onkeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') confirming = null;
  }

  const focusOnMount = (node: HTMLElement) => node.focus();
</script>

{#if !control.enabled}
  <p class="off">
    <svg viewBox="0 0 16 16" aria-hidden="true"><rect x="3.5" y="7" width="9" height="6.5" rx="1.2" /><path d="M5.5 7V5a2.5 2.5 0 0 1 5 0v2" /></svg>
    <span>Controls are disabled on this server{control.reason ? `: ${control.reason}` : '.'}</span>
  </p>
{:else}
  <div class="controls" role="group" aria-label="App controls">
    {#if busy}
      <span class="busy" role="status">
        <Spinner label="{VERB[busy.action][1]} {appName}" />
        <span>{VERB[busy.action][1]} <strong>{appName}</strong>…</span>
        <span class="num faint">{duration(Math.max(0, env.now - busy.since))}</span>
        <span class="hint">can take up to 90s</span>
      </span>
    {:else if confirming}
      <span class="ask">
        <span>{VERB[confirming][0]} <strong>{appName}</strong>?</span>
        {#if confirming === 'stop'}<span class="hint">It stays down until started again.</span>{/if}
      </span>
      <button type="button" class="btn" onclick={() => (confirming = null)} {onkeydown}>Cancel</button>
      <button
        type="button"
        class="btn danger solid"
        {@attach focusOnMount}
        onclick={() => confirming && run(confirming)}
        {onkeydown}>Yes, {confirming}</button
      >
    {:else if actions.length}
      {#each actions as a (a)}
        <button type="button" class="btn act {a}" onclick={() => ask(a)}>
          <svg viewBox="0 0 16 16" aria-hidden="true">
            {#if a === 'start'}
              <path d="M5 3.5v9l7-4.5z" />
            {:else if a === 'stop'}
              <rect x="4" y="4" width="8" height="8" rx="1" />
            {:else}
              <path d="M12.5 8a4.5 4.5 0 1 1-1.3-3.2M12.5 3v2.5H10" />
            {/if}
          </svg>
          {VERB[a][0]}
        </button>
      {/each}
    {:else}
      <p class="off inline">{control.reason || 'No actions available for this app right now.'}</p>
    {/if}
  </div>

  {#if result}
    <div class="result" class:ok={result.ok} role={result.ok ? 'status' : 'alert'}>
      <div class="msg">
        <span>{result.message}</span>
        <button type="button" class="x" aria-label="Dismiss" onclick={() => (result = null)}>×</button>
      </div>
      {#if result.ok && result.output.trim()}
        <details>
          <summary>Output</summary>
          <pre>{result.output.trim()}</pre>
        </details>
      {:else if !result.ok}
        <pre>{result.output}</pre>
      {/if}
    </div>
  {/if}
{/if}

<style>
  .controls {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px;
    min-height: 32px;
  }
  .act svg {
    width: 13px;
    height: 13px;
    fill: none;
    stroke: currentColor;
    stroke-width: 1.6;
    stroke-linecap: round;
    stroke-linejoin: round;
  }
  .act.start {
    color: var(--up);
    border-color: color-mix(in srgb, var(--up) 45%, var(--border-strong));
  }
  .act.start svg {
    fill: currentColor;
    stroke: none;
  }
  .act.stop {
    color: var(--down);
  }
  .busy,
  .ask {
    display: inline-flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px;
    font-size: 13px;
    color: var(--text-2);
  }
  .busy {
    color: var(--text);
  }
  .ask {
    margin-right: 4px;
  }
  .hint {
    font-size: 12px;
    color: var(--muted);
  }
  .off {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: 12.5px;
    color: var(--muted);
  }
  .off svg {
    flex: none;
    width: 13px;
    height: 13px;
    fill: none;
    stroke: currentColor;
    stroke-width: 1.4;
  }
  .result {
    margin-top: 2px;
    border: 1px solid color-mix(in srgb, var(--down) 40%, var(--border));
    border-radius: var(--radius-sm);
    background: color-mix(in srgb, var(--down) 5%, var(--surface));
    font-size: 13px;
    overflow: hidden;
  }
  .result.ok {
    border-color: color-mix(in srgb, var(--up) 40%, var(--border));
    background: color-mix(in srgb, var(--up) 5%, var(--surface));
  }
  .msg {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding: 7px 8px 7px 12px;
    color: var(--down);
  }
  .ok .msg {
    color: var(--text-2);
  }
  .x {
    border: 0;
    background: transparent;
    color: var(--muted);
    font-size: 17px;
    line-height: 1;
    width: 24px;
    height: 24px;
    border-radius: 4px;
    cursor: pointer;
  }
  .x:hover {
    background: var(--surface-2);
    color: var(--text);
  }
  details {
    border-top: 1px solid var(--border);
  }
  summary {
    padding: 6px 12px;
    font-size: 12px;
    color: var(--muted);
    cursor: pointer;
  }
  pre {
    margin: 0;
    max-height: 220px;
    overflow: auto;
    padding: 10px 12px;
    border-top: 1px solid var(--border);
    font-family: var(--font-mono);
    font-size: 12px;
    line-height: 1.5;
    white-space: pre-wrap;
    word-break: break-word;
    color: var(--text-2);
  }
</style>
