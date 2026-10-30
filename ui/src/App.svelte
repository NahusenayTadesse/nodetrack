<script lang="ts">
  import { onMount } from 'svelte';
  import { live } from './lib/live.svelte';
  import { router, homeHref } from './lib/router.svelte';
  import ConnIndicator from './components/ConnIndicator.svelte';
  import Overview from './routes/Overview.svelte';

  // The detail page pulls in uPlot; load it on demand so the overview stays tiny.
  let detailModule: Promise<typeof import('./routes/AppDetail.svelte')> | undefined;
  const loadDetail = () => (detailModule ??= import('./routes/AppDetail.svelte'));

  onMount(() => {
    live.start();
    return () => live.stop();
  });

  const route = $derived(router.route);
</script>

<header class="top">
  <div class="wrap bar">
    <a class="brand" href={homeHref} aria-label="nodetrack overview">
      <svg viewBox="0 0 32 32" aria-hidden="true" class="logo">
        <rect width="32" height="32" rx="8" />
        <polyline points="5,19 11,19 14,9 18,24 21,15 27,15" />
      </svg>
      <span class="word">node<span>track</span></span>
    </a>
    <ConnIndicator />
  </div>
</header>

<main class="wrap">
  {#if route.name === 'overview'}
    <Overview />
  {:else if route.name === 'app'}
    {#await loadDetail()}
      <p class="muted pad">Loading…</p>
    {:then { default: AppDetail }}
      {#key route.id}
        <AppDetail id={route.id} />
      {/key}
    {:catch err}
      <p class="pad err">Failed to load page: {err.message}</p>
    {/await}
  {:else}
    <div class="notfound">
      <h1>Nothing at <code>{route.path}</code></h1>
      <a class="btn" href={homeHref}>Back to overview</a>
    </div>
  {/if}
</main>

<footer class="wrap foot">
  {#if live.data}
    <span class="num">nodetrack {live.data.tracker.version}</span>
    <span class="faint">·</span>
  {/if}
  <span>Node.js &amp; Bun process monitor</span>
</footer>

<style>
  .top {
    position: sticky;
    top: 0;
    z-index: 10;
    background: color-mix(in srgb, var(--bg) 86%, transparent);
    backdrop-filter: saturate(1.4) blur(10px);
    -webkit-backdrop-filter: saturate(1.4) blur(10px);
    border-bottom: 1px solid var(--border);
  }
  .bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    height: 54px;
    padding-top: env(safe-area-inset-top);
  }
  .brand {
    display: inline-flex;
    align-items: center;
    gap: 9px;
  }
  .logo {
    width: 24px;
    height: 24px;
    flex: none;
  }
  .logo rect {
    fill: var(--text);
  }
  .logo polyline {
    fill: none;
    stroke: var(--bg);
    stroke-width: 3;
    stroke-linecap: round;
    stroke-linejoin: round;
  }
  .word {
    font-family: var(--font-mono);
    font-size: 15px;
    font-weight: 600;
    letter-spacing: -0.03em;
  }
  .word span {
    color: var(--accent);
  }
  main {
    padding-top: 20px;
    padding-bottom: 28px;
    min-height: calc(100dvh - 54px - 64px);
  }
  .pad {
    padding: 48px 0;
    text-align: center;
  }
  .err {
    color: var(--down);
  }
  .notfound {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 14px;
    padding: 80px 0;
  }
  .notfound h1 {
    font-size: 18px;
    font-weight: 600;
    overflow-wrap: anywhere;
    text-align: center;
  }
  .foot {
    display: flex;
    gap: 8px;
    justify-content: center;
    flex-wrap: wrap;
    padding-top: 8px;
    padding-bottom: max(28px, env(safe-area-inset-bottom));
    font-size: 12px;
    color: var(--faint);
  }
  @media (max-width: 760px) {
    main {
      padding-top: 14px;
    }
  }
</style>
