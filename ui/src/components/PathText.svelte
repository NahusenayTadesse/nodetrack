<script lang="ts">
  import { splitPath } from '../lib/format';
  import CopyButton from './CopyButton.svelte';

  /** Shows a path that ellipsizes in the middle (the last segment stays visible). */
  let { value, copy = true, href }: { value: string; copy?: boolean; href?: string } = $props();

  const parts = $derived(splitPath(value));
</script>

<span class="path" title={value}>
  {#if href}
    <a class="text mono link" {href} target="_blank" rel="noopener noreferrer">
      <span class="head">{parts[0]}</span><span class="tail">{parts[1]}</span>
    </a>
  {:else}
    <span class="text mono"><span class="head">{parts[0]}</span><span class="tail">{parts[1]}</span></span>
  {/if}
  {#if copy}<CopyButton text={value} label="Copy {value}" />{/if}
</span>

<style>
  .path {
    display: flex;
    align-items: center;
    gap: 4px;
    min-width: 0;
  }
  .text {
    display: flex;
    min-width: 0;
    white-space: nowrap;
    color: var(--text-2);
  }
  .head {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .tail {
    flex: none;
    max-width: 75%;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .text:has(.head:empty) .tail {
    max-width: 100%;
  }
  .link {
    color: var(--accent);
  }
  .link:hover .tail,
  .link:hover .head {
    text-decoration: underline;
  }
</style>
