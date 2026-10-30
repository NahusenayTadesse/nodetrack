<script lang="ts">
  interface Props {
    values: number[];
    /** CSS color for the line */
    color?: string;
    /** CSS color for the area under the line */
    fill?: string;
    /** fixed lower bound (defaults to the data minimum) */
    min?: number;
    /** smallest vertical range, so flat-ish data doesn't look dramatic */
    minRange?: number;
    /** number of x slots; fewer samples are right-aligned (newest at right) */
    slots?: number;
    label?: string;
    muted?: boolean;
  }

  let {
    values,
    color = 'var(--cpu)',
    fill = 'var(--cpu-fill)',
    min,
    minRange = 1,
    slots = 60,
    label = '',
    muted = false
  }: Props = $props();

  const W = 100;
  const H = 28;
  const PAD = 2.5;

  const geom = $derived.by(() => {
    const v = values.filter((x) => Number.isFinite(x));
    if (v.length < 2) return null;
    let lo = min ?? Math.min(...v);
    let hi = Math.max(...v);
    if (hi - lo < minRange) {
      // nearly flat: centre the line (or pin it to the fixed floor)
      if (min === undefined) lo = Math.max(0, (lo + hi) / 2 - minRange / 2);
      hi = lo + minRange;
    }
    const span = Math.max(slots, v.length) - 1;
    const step = W / span;
    const pts = v.map((y, i) => {
      const px = W - (v.length - 1 - i) * step;
      const py = H - PAD - ((y - lo) / (hi - lo)) * (H - PAD * 2);
      return [+px.toFixed(2), +py.toFixed(2)] as const;
    });
    const line = pts.map((p) => p.join(',')).join(' ');
    const area = `${pts[0][0]},${H} ${line} ${W},${H}`;
    const last = pts[pts.length - 1];
    return { line, area, last };
  });
</script>

<div class="sw">
<svg
  class="spark"
  class:muted
  viewBox="0 0 {W} {H}"
  preserveAspectRatio="none"
  role="img"
  aria-label={label}
  style:--c={color}
  style:--f={fill}
>
  {#if geom}
    <polygon points={geom.area} class="area" />
    <polyline points={geom.line} class="line" vector-effect="non-scaling-stroke" />
  {:else}
    <line x1="0" x2={W} y1={H - PAD} y2={H - PAD} class="empty" vector-effect="non-scaling-stroke" />
  {/if}
</svg>
{#if geom}
  <!-- the end-point dot lives outside the stretched svg so it stays round -->
  <span class="tip" style:left="calc({geom.last[0]}% - 2.5px)" style:top="calc({(geom.last[1] / H) * 100}% - 2.5px)" style:--c={color}
  ></span>
{/if}
</div>

<style>
  .sw {
    position: relative;
    width: 100%;
    height: 100%;
  }
  .spark {
    display: block;
    width: 100%;
    height: 100%;
    overflow: visible;
  }
  .area {
    fill: var(--f);
  }
  .line {
    fill: none;
    stroke: var(--c);
    stroke-width: 1.5;
    stroke-linejoin: round;
    stroke-linecap: round;
  }
  .empty {
    stroke: var(--border-strong);
    stroke-width: 1;
    stroke-dasharray: 2 3;
  }
  .muted .line {
    stroke: var(--faint);
  }
  .muted .area {
    fill: var(--surface-2);
  }
  .tip {
    position: absolute;
    width: 5px;
    height: 5px;
    border-radius: 50%;
    background: var(--c);
    pointer-events: none;
  }
  .muted + .tip {
    background: var(--faint);
  }
</style>
