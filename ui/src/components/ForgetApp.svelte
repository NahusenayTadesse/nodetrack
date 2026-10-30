<script lang="ts">
  import { ApiError, forgetApp } from '../lib/api';

  let { id, name, onforgotten }: { id: number; name: string; onforgotten: () => void } = $props();

  let phase = $state<'idle' | 'confirm' | 'busy'>('idle');
  let error = $state<string | null>(null);
  // Move focus to the destructive action once it appears.
  const focusOnMount = (node: HTMLElement) => node.focus();

  async function confirm() {
    phase = 'busy';
    error = null;
    try {
      await forgetApp(id);
      onforgotten();
    } catch (e) {
      phase = 'idle';
      if (e instanceof ApiError && e.status === 409) {
        error = `${name} is running again. Only stopped apps can be forgotten.`;
      } else {
        error = `Could not forget app: ${e instanceof Error ? e.message : String(e)}`;
      }
    }
  }

  function onkeydown(e: KeyboardEvent) {
    if (e.key === 'Escape' && phase === 'confirm') phase = 'idle';
  }
</script>

<div class="forget" role="group" aria-label="Forget app">
  <div class="text">
    <h3>Forget this app</h3>
    {#if phase === 'idle'}
      <p>{name} is stopped. Forgetting removes it and its recorded history from nodetrack.</p>
    {:else}
      <p class="ask">
        Remove <strong>{name}</strong> and all of its history? This cannot be undone. If it starts again it will be
        rediscovered as a new app.
      </p>
    {/if}
    {#if error}<p class="err" role="alert">{error}</p>{/if}
  </div>
  <div class="actions">
    {#if phase === 'idle'}
      <button type="button" class="btn danger" onclick={() => (phase = 'confirm')}>Forget app…</button>
    {:else}
      <button type="button" class="btn" disabled={phase === 'busy'} onclick={() => (phase = 'idle')} {onkeydown}
        >Cancel</button
      >
      <button type="button" class="btn danger solid" {@attach focusOnMount}
        disabled={phase === 'busy'}
        onclick={confirm}
        {onkeydown}
      >
        {phase === 'busy' ? 'Forgetting…' : 'Yes, forget it'}
      </button>
    {/if}
  </div>
</div>

<style>
  .forget {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    flex-wrap: wrap;
    padding: 16px;
    border: 1px solid color-mix(in srgb, var(--down) 35%, var(--border));
    border-radius: var(--radius);
    background: color-mix(in srgb, var(--down) 4%, var(--surface));
  }
  .text {
    display: flex;
    flex-direction: column;
    gap: 4px;
    min-width: 0;
    flex: 1 1 280px;
  }
  h3 {
    font-size: 14px;
    font-weight: 600;
  }
  p {
    font-size: 13px;
    color: var(--text-2);
  }
  .ask strong {
    color: var(--down);
  }
  .err {
    color: var(--down);
  }
  .actions {
    display: flex;
    gap: 8px;
  }
</style>
