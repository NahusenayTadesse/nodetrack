<script lang="ts">
  import { coreList } from '../lib/format';

  let { cores, cpus, dim = false }: { cores: number[]; cpus: number; dim?: boolean } = $props();

  const used = $derived(new Set(cores));
  const title = $derived(cores.length ? `Last ran on CPU ${coreList(cores)} of ${cpus}` : 'Not running');
</script>

{#if cpus > 0 && cpus <= 32}
  <span class="map" class:dim class:wide={cpus > 8} {title} role="img" aria-label={title}>
    {#each { length: cpus }, i (i)}
      <span class="core" class:on={used.has(i)}></span>
    {/each}
  </span>
{:else}
  <span class="num text" {title}>{coreList(cores)}</span>
{/if}

<style>
  .map {
    display: inline-grid;
    grid-template-columns: repeat(4, 7px);
    gap: 2px;
    vertical-align: middle;
  }
  .map.wide {
    grid-template-columns: repeat(8, 7px);
  }
  .core {
    width: 7px;
    height: 7px;
    border-radius: 2px;
    background: var(--surface-3);
  }
  .core.on {
    background: var(--cpu);
  }
  .dim .core.on {
    background: var(--faint);
  }
  .text {
    font-size: 12px;
  }
</style>
