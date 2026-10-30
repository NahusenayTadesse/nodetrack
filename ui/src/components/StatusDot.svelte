<script lang="ts">
  import type { Status } from '../lib/api';

  let { status, pulse = true }: { status: Status; pulse?: boolean } = $props();
</script>

<span class="dot {status}" class:pulse={pulse && status === 'up'} role="img" aria-label={status === 'up' ? 'Up' : 'Down'}
></span>

<style>
  .dot {
    position: relative;
    display: inline-block;
    flex: none;
    width: 8px;
    height: 8px;
    border-radius: 50%;
  }
  .up {
    background: var(--up);
  }
  .down {
    background: transparent;
    box-shadow: inset 0 0 0 1.5px var(--down);
  }
  .pulse::after {
    content: '';
    position: absolute;
    inset: 0;
    border-radius: 50%;
    background: var(--up);
    animation: ping 2.4s cubic-bezier(0, 0, 0.2, 1) infinite;
  }
  @keyframes ping {
    0% {
      transform: scale(1);
      opacity: 0.55;
    }
    70%,
    100% {
      transform: scale(2.6);
      opacity: 0;
    }
  }
</style>
