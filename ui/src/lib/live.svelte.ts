import { getOverview, normalizeOverview, STREAM_URL, type Overview } from './api';

export type ConnState = 'connecting' | 'live' | 'reconnecting' | 'offline';

/**
 * Overview state fed by one initial fetch (instant first paint) and then the
 * `/api/stream` SSE feed. EventSource reconnects on its own; we only step in
 * when the browser gives up (readyState CLOSED, e.g. after a non-200 reply).
 */
class LiveOverview {
  data = $state<Overview | null>(null);
  conn = $state<ConnState>('connecting');
  /** unix ms of the last snapshot received */
  updatedAt = $state(0);
  error = $state<string | null>(null);

  #es: EventSource | null = null;
  #retry: ReturnType<typeof setTimeout> | undefined;
  #backoff = 1000;
  #started = false;

  start() {
    if (this.#started) return;
    this.#started = true;
    getOverview()
      .then((o) => {
        // Don't clobber a newer snapshot that already arrived via SSE.
        if (!this.data) this.#apply(o);
      })
      .catch((e: unknown) => {
        if (!this.data) this.error = e instanceof Error ? e.message : String(e);
      });
    this.#connect();
  }

  #apply(o: Overview) {
    this.data = o;
    this.updatedAt = Date.now();
    this.error = null;
  }

  #connect() {
    clearTimeout(this.#retry);
    const es = new EventSource(STREAM_URL);
    this.#es = es;

    es.onopen = () => {
      this.conn = 'live';
      this.#backoff = 1000;
    };
    es.addEventListener('overview', (ev) => {
      try {
        this.#apply(normalizeOverview(JSON.parse((ev as MessageEvent<string>).data)));
        this.conn = 'live';
      } catch {
        /* malformed frame: keep the previous snapshot */
      }
    });
    es.onerror = () => {
      if (es.readyState === EventSource.CLOSED) {
        // Browser gave up: reconnect ourselves with capped backoff.
        this.conn = 'offline';
        es.close();
        this.#retry = setTimeout(() => this.#connect(), this.#backoff);
        this.#backoff = Math.min(this.#backoff * 2, 30_000);
      } else {
        this.conn = 'reconnecting';
      }
    };
  }

  stop() {
    clearTimeout(this.#retry);
    this.#es?.close();
    this.#es = null;
    this.#started = false;
  }
}

export const live = new LiveOverview();
