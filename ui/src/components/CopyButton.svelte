<script lang="ts">
  let { text, label = 'Copy' }: { text: string; label?: string } = $props();

  let copied = $state(false);
  let timer: ReturnType<typeof setTimeout> | undefined;

  // navigator.clipboard needs a secure context; the dashboard is often served
  // over plain http on the VPS IP, so fall back to a hidden textarea.
  function legacyCopy(value: string): boolean {
    const ta = document.createElement('textarea');
    ta.value = value;
    ta.setAttribute('readonly', '');
    ta.style.position = 'fixed';
    ta.style.opacity = '0';
    document.body.appendChild(ta);
    ta.select();
    let ok = false;
    try {
      ok = document.execCommand('copy');
    } catch {
      ok = false;
    }
    ta.remove();
    return ok;
  }

  async function copy() {
    let ok = false;
    try {
      if (navigator.clipboard && window.isSecureContext) {
        await navigator.clipboard.writeText(text);
        ok = true;
      }
    } catch {
      ok = false;
    }
    if (!ok) ok = legacyCopy(text);
    if (!ok) return;
    copied = true;
    clearTimeout(timer);
    timer = setTimeout(() => (copied = false), 1400);
  }
</script>

<button type="button" class="copy" class:copied onclick={copy} title={copied ? 'Copied' : label} aria-label={label}>
  {#if copied}
    <svg viewBox="0 0 16 16" aria-hidden="true"><path d="M3.5 8.5 6.5 11.5 12.5 4.5" /></svg>
  {:else}
    <svg viewBox="0 0 16 16" aria-hidden="true"
      ><rect x="5.5" y="5.5" width="8" height="8" rx="1.5" /><path d="M10.5 3.5v-.5a1 1 0 0 0-1-1h-6a1 1 0 0 0-1 1v6a1 1 0 0 0 1 1h.5" /></svg
    >
  {/if}
  <span class="visually-hidden" aria-live="polite">{copied ? 'Copied' : ''}</span>
</button>

<style>
  .copy {
    position: relative;
    z-index: 1;
    display: inline-grid;
    place-items: center;
    flex: none;
    width: 24px;
    height: 24px;
    padding: 0;
    border: 1px solid transparent;
    border-radius: 5px;
    background: transparent;
    color: var(--faint);
    cursor: pointer;
  }
  .copy:hover {
    color: var(--text);
    background: var(--surface-2);
    border-color: var(--border);
  }
  .copy.copied {
    color: var(--up);
  }
  svg {
    width: 14px;
    height: 14px;
    fill: none;
    stroke: currentColor;
    stroke-width: 1.4;
    stroke-linecap: round;
    stroke-linejoin: round;
  }
</style>
