<script lang="ts">
  import { untrack } from 'svelte';
  import type { Attachment } from 'svelte/attachments';
  import uPlot from 'uplot';
  import 'uplot/dist/uPlot.min.css';
  import { env, cssVar } from '../lib/env.svelte';
  import { bytes, pct, shortTime } from '../lib/format';

  interface Props {
    kind: 'cpu' | 'mem';
    title: string;
    ts: number[];
    avg: (number | null)[];
    peak: (number | null)[];
    /** true while a new range is being fetched */
    loading?: boolean;
  }

  let { kind, title, ts, avg, peak, loading = false }: Props = $props();

  const data = $derived([ts, avg, peak] as uPlot.AlignedData);
  const empty = $derived(ts.length === 0);

  const KiB = 1024;
  // binary-friendly tick increments for the memory axis: 1, 2, 5 x 2^(10k)
  const BYTE_INCRS = [KiB, KiB ** 2, KiB ** 3, KiB ** 4].flatMap((u) =>
    [1, 2, 5, 10, 20, 25, 50, 100, 200, 250, 500].map((m) => m * u)
  );

  const fmt = (v: number) => (kind === 'cpu' ? pct(v) : bytes(v));
  const axisFmt = (v: number) => (kind === 'cpu' ? `${+v.toFixed(1)}%` : bytes(v).replace('.0 ', ' '));

  function chartHeight(width: number) {
    return width < 520 ? 170 : 210;
  }

  function makeOpts(width: number): uPlot.Options {
    const line = cssVar(kind === 'cpu' ? '--cpu' : '--mem');
    const fill = cssVar(kind === 'cpu' ? '--cpu-fill' : '--mem-fill');
    const max = cssVar(kind === 'cpu' ? '--cpu-max' : '--mem-max');
    const grid = cssVar('--grid');
    const axis = cssVar('--axis');
    const font = `11px ${cssVar('--font-mono')}`;

    const axisBase = {
      stroke: axis,
      font,
      grid: { stroke: grid, width: 1 },
      ticks: { stroke: grid, width: 1, size: 4 }
    };

    return {
      width,
      height: chartHeight(width),
      padding: [10, 6, 0, 0],
      cursor: {
        drag: { x: true, y: false },
        points: { size: 7, width: 2 }
      },
      legend: { live: true },
      scales: {
        x: { time: true },
        y: {
          range: (_u, _min, dmax) => {
            const top = kind === 'cpu' ? Math.max(5, (dmax || 0) * 1.12) : Math.max(KiB ** 2, (dmax || 0) * 1.15);
            return [0, top];
          }
        }
      },
      axes: [
        { ...axisBase, space: 70 },
        {
          ...axisBase,
          size: kind === 'cpu' ? 52 : 70,
          space: 32,
          incrs: kind === 'mem' ? BYTE_INCRS : undefined,
          values: (_u, splits) => splits.map(axisFmt)
        }
      ],
      series: [
        { label: 'time', value: (_u, t) => (t == null ? '–' : shortTime(t)) },
        {
          label: 'avg',
          stroke: line,
          fill,
          width: 1.75,
          points: { show: false },
          value: (_u, v) => (v == null ? '–' : fmt(v))
        },
        {
          label: 'peak',
          stroke: max,
          width: 1,
          dash: [3, 3],
          points: { show: false },
          value: (_u, v) => (v == null ? '–' : fmt(v))
        }
      ]
    };
  }

  // The plot is (re)built when the colour scheme flips, since canvas colours are
  // baked in from the CSS variables at creation. Data changes only call setData.
  const attachPlot: Attachment<HTMLDivElement> = (target) => {
    void env.dark;
    const u = new uPlot(makeOpts(target.clientWidth || 600), untrack(() => data), target);

    $effect(() => {
      const d = data;
      if (u.data !== d) u.setData(d);
    });

    const ro = new ResizeObserver((entries) => {
      const w = Math.floor(entries[0].contentRect.width);
      if (w > 0 && w !== u.width) u.setSize({ width: w, height: chartHeight(w) });
    });
    ro.observe(target);

    return () => {
      ro.disconnect();
      u.destroy();
    };
  };
</script>

<figure class="chart {kind}" class:loading>
  <figcaption>
    <span class="swatch" aria-hidden="true"></span>
    <span class="t">{title}</span>
  </figcaption>
  <div class="plot" {@attach attachPlot}></div>
  {#if empty && !loading}
    <p class="empty">No samples in this range yet</p>
  {/if}
</figure>

<style>
  .chart {
    position: relative;
    margin: 0;
    min-width: 0;
    padding: 14px 14px 8px;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--surface);
    box-shadow: var(--shadow);
  }
  figcaption {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 6px;
    font-size: 13px;
    font-weight: 600;
  }
  .swatch {
    width: 10px;
    height: 3px;
    border-radius: 2px;
    background: var(--cpu);
  }
  .mem .swatch {
    background: var(--mem);
  }
  .plot {
    width: 100%;
    min-width: 0;
    overflow: hidden;
    transition: opacity 0.2s;
  }
  .loading .plot {
    opacity: 0.45;
  }
  .empty {
    position: absolute;
    inset: 40px 0 40px;
    display: grid;
    place-items: center;
    font-size: 12.5px;
    color: var(--muted);
    pointer-events: none;
  }

  /* uPlot theming */
  .plot :global(.u-legend) {
    font-family: var(--font-mono);
    font-size: 11.5px;
    color: var(--muted);
    text-align: left;
    padding: 2px 0 0;
  }
  .plot :global(.u-legend .u-series > *) {
    padding: 2px 4px;
  }
  .plot :global(.u-legend .u-value) {
    color: var(--text);
  }
  .plot :global(.u-legend th) {
    font-weight: 500;
  }
  .plot :global(.u-select) {
    background: color-mix(in srgb, var(--text) 8%, transparent);
  }
  .plot :global(.u-cursor-x),
  .plot :global(.u-cursor-y) {
    border-color: var(--border-strong) !important;
  }
  .plot :global(.u-cursor-y) {
    display: none;
  }
</style>
