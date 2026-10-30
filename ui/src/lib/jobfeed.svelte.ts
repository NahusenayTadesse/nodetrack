import { getJob, jobStreamUrl, type Job } from './api';

/**
 * Output + status of one job. Running jobs are followed over SSE
 * (`output` = JSON string chunk, `done` = final Job); finished jobs are
 * loaded once with GET /api/jobs/{id}.
 *
 * Chunks are batched per animation frame so a chatty `npm install` doesn't
 * re-render the console hundreds of times per second.
 */
export class JobFeed {
  job = $state<Job>() as Job;
  raw = $state('');
  error = $state<string | null>(null);
  reconnecting = $state(false);
  loading = $state(false);

  #es: EventSource | null = null;
  #buf = '';
  #reset = false;
  #frame = 0;
  #ctrl: AbortController | null = null;
  #onfinished: (job: Job) => void;

  constructor(initial: Job, onfinished: (job: Job) => void = () => {}) {
    this.job = initial;
    this.#onfinished = onfinished;
  }

  get running() {
    return this.job.status === 'running';
  }

  start() {
    if (this.running) this.#follow();
    else this.#load();
  }

  async #load() {
    this.#ctrl = new AbortController();
    this.loading = true;
    try {
      const j = await getJob(this.job.id, this.#ctrl.signal);
      this.job = j;
      this.raw = j.output ?? '';
      this.error = null;
      // it may have been started elsewhere and still be running
      if (j.status === 'running') this.#follow();
    } catch (e) {
      if (!this.#ctrl.signal.aborted) this.error = e instanceof Error ? e.message : String(e);
    } finally {
      this.loading = false;
    }
  }

  #follow() {
    const es = new EventSource(jobStreamUrl(this.job.id));
    this.#es = es;

    // Each (re)connect replays everything so far: replace instead of append.
    es.onopen = () => {
      this.#reset = true;
      this.#buf = '';
      this.reconnecting = false;
      this.error = null;
    };
    es.addEventListener('output', (ev) => {
      let chunk: unknown;
      try {
        chunk = JSON.parse((ev as MessageEvent<string>).data);
      } catch {
        chunk = (ev as MessageEvent<string>).data;
      }
      if (typeof chunk !== 'string') return;
      this.#buf += chunk;
      this.#schedule();
    });
    es.addEventListener('done', (ev) => {
      try {
        const final = JSON.parse((ev as MessageEvent<string>).data) as Job;
        this.job = { ...this.job, ...final };
      } catch {
        /* keep what we have */
      }
      this.#flush();
      this.#closeStream();
      this.#onfinished(this.job);
    });
    es.onerror = () => {
      if (es.readyState === EventSource.CLOSED) {
        this.#closeStream();
        if (this.running) this.error = 'Lost the output stream.';
      } else {
        this.reconnecting = true;
      }
    };
  }

  #schedule() {
    if (this.#frame) return;
    this.#frame = requestAnimationFrame(() => this.#flush());
  }

  #flush() {
    cancelAnimationFrame(this.#frame);
    this.#frame = 0;
    if (!this.#buf && !this.#reset) return;
    this.raw = this.#reset ? this.#buf : this.raw + this.#buf;
    this.#reset = false;
    this.#buf = '';
  }

  #closeStream() {
    this.#es?.close();
    this.#es = null;
    this.reconnecting = false;
  }

  /** Merge a Job returned by e.g. the cancel endpoint. */
  update(j: Job) {
    this.job = { ...this.job, ...j };
  }

  destroy() {
    this.#ctrl?.abort();
    this.#closeStream();
    cancelAnimationFrame(this.#frame);
  }
}
