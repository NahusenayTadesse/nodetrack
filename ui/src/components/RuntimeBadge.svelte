<script lang="ts">
  import type { Runtime } from '../lib/api';

  let { runtime }: { runtime: Runtime } = $props();
</script>

<span class="badge {runtime}" title={runtime === 'bun' ? 'Bun runtime' : 'Node.js runtime'}>{runtime}</span>

<style>
  .badge {
    display: inline-flex;
    align-items: center;
    height: 18px;
    padding: 0 6px;
    border-radius: 4px;
    font-family: var(--font-mono);
    font-size: 10.5px;
    font-weight: 600;
    letter-spacing: 0.02em;
    text-transform: lowercase;
    background: var(--surface-2);
    color: var(--text-2);
  }
  .node {
    background: var(--node-bg);
    color: var(--node-fg);
  }
  .bun {
    background: var(--bun-bg);
    color: var(--bun-fg);
  }
</style>
