<script lang="ts">
  let { size = 14, label = 'Working' }: { size?: number; label?: string } = $props();
</script>

<span class="spin" role="status" aria-label={label} style:--s="{size}px"></span>

<style>
  .spin {
    display: inline-block;
    flex: none;
    width: var(--s);
    height: var(--s);
    border-radius: 50%;
    border: 2px solid color-mix(in srgb, currentColor 22%, transparent);
    border-top-color: currentColor;
    animation: spin 0.75s linear infinite;
    vertical-align: middle;
  }
  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
</style>
