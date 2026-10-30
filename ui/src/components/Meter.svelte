<script lang="ts">
  let { value, label }: { value: number; label: string } = $props();

  const clamped = $derived(Math.max(0, Math.min(100, value)));
  const level = $derived(clamped >= 92 ? 'crit' : clamped >= 80 ? 'warn' : 'ok');
</script>

<div
  class="meter {level}"
  role="meter"
  aria-label={label}
  aria-valuemin={0}
  aria-valuemax={100}
  aria-valuenow={Math.round(clamped)}
>
  <div class="fill" style:width="{clamped}%"></div>
</div>

<style>
  .meter {
    height: 4px;
    border-radius: 2px;
    background: var(--surface-3);
    overflow: hidden;
  }
  .fill {
    height: 100%;
    border-radius: 2px;
    background: var(--text-2);
    transition: width 0.6s cubic-bezier(0.2, 0.7, 0.2, 1);
  }
  .warn .fill {
    background: var(--warn);
  }
  .crit .fill {
    background: var(--down);
  }
</style>
