<script lang="ts">
  import { live } from '../lib/live.svelte';
  import { env } from '../lib/env.svelte';
  import { relative } from '../lib/format';

  const LABEL = {
    connecting: 'connecting',
    live: 'live',
    reconnecting: 'reconnecting',
    offline: 'offline'
  } as const;

  const title = $derived(
    live.updatedAt ? `Last update ${relative(Math.floor(live.updatedAt / 1000), env.now)}` : 'Waiting for first update'
  );
</script>

<span class="conn {live.conn}" {title} role="status" aria-live="polite">
  <span class="led"></span>
  <span class="txt">{LABEL[live.conn]}</span>
</span>

<style>
  .conn {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    height: 26px;
    padding: 0 10px 0 9px;
    border-radius: 999px;
    border: 1px solid var(--border);
    background: var(--surface);
    font-family: var(--font-mono);
    font-size: 11.5px;
    color: var(--muted);
    white-space: nowrap;
  }
  .led {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--faint);
  }
  .live .led {
    background: var(--up);
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--up) 22%, transparent);
    animation: breathe 2.4s ease-in-out infinite;
  }
  .live .txt {
    color: var(--text-2);
  }
  .reconnecting .led,
  .connecting .led {
    background: var(--warn);
    animation: blink 1s steps(2, start) infinite;
  }
  .reconnecting {
    color: var(--warn);
    border-color: color-mix(in srgb, var(--warn) 40%, var(--border));
  }
  .offline {
    color: var(--down);
    border-color: color-mix(in srgb, var(--down) 40%, var(--border));
  }
  .offline .led {
    background: var(--down);
  }
  @keyframes breathe {
    50% {
      box-shadow: 0 0 0 5px color-mix(in srgb, var(--up) 6%, transparent);
    }
  }
  @keyframes blink {
    to {
      visibility: hidden;
    }
  }
</style>
