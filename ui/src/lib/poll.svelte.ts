/**
 * A value fetched now and then re-fetched on an interval, owned by the
 * component that creates it (must be constructed during component init).
 *
 * `source()` is reactive: when the state it reads changes (e.g. the selected
 * range), in-flight requests are aborted and polling restarts immediately.
 */
export class Poll<T> {
  value = $state<T | undefined>();
  error = $state<unknown>();
  /** true until the first response for the current source arrives */
  pending = $state(true);
  /** polling stopped because `stopOn` matched */
  stopped = $state(false);
  #nonce = $state(0);

  constructor(opts: {
    source: () => (signal: AbortSignal) => Promise<T>;
    every: () => number;
    stopOn?: (err: unknown) => boolean;
  }) {
    $effect(() => {
      void this.#nonce;
      const run = opts.source();
      const ms = opts.every();
      const ctrl = new AbortController();
      let timer: ReturnType<typeof setTimeout> | undefined;
      this.pending = true;
      this.stopped = false;

      const tick = async () => {
        try {
          const v = await run(ctrl.signal);
          if (ctrl.signal.aborted) return;
          this.value = v;
          this.error = undefined;
        } catch (e) {
          if (ctrl.signal.aborted) return;
          this.error = e;
          if (opts.stopOn?.(e)) {
            this.pending = false;
            this.stopped = true;
            return;
          }
        }
        this.pending = false;
        timer = setTimeout(tick, ms);
      };
      tick();

      return () => {
        ctrl.abort();
        clearTimeout(timer);
      };
    });
  }

  /** Fetch again right away (keeps the current value until the new one lands). */
  refresh() {
    this.#nonce++;
  }
}
