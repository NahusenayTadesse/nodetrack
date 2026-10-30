// Types and fetch helpers for the nodetrack HTTP API (see docs/API.md).
// Sizes are bytes, times are unix seconds, CPU is percent of ONE core.

export type Runtime = 'node' | 'bun';
export type Status = 'up' | 'down';
export type Source = 'pm2' | 'systemd' | 'manual';
export type EventKind = 'discovered' | 'start' | 'stop' | 'restart' | 'action' | 'job';
export type Action = 'start' | 'stop' | 'restart';
export type PackageManager = 'npm' | 'pnpm' | 'yarn' | 'bun';
export type JobStatus = 'running' | 'ok' | 'failed' | 'canceled';
export type Range = '1h' | '6h' | '24h' | '7d' | '30d';

export const RANGES: Range[] = ['1h', '6h', '24h', '7d', '30d'];

export interface Host {
  hostname: string;
  cpus: number;
  cpuPct: number;
  memTotal: number;
  memAvailable: number;
  load: [number, number, number];
  uptime: number;
}

export interface Tracker {
  rss: number;
  interval: number;
  version: string;
  /** v0.2: true when nodetrack runs with -controls and basic auth */
  controls?: boolean;
}

export interface Port {
  port: number;
  addr: string;
}

export interface Domain {
  host: string;
  path: string;
  source: string;
  tls: boolean;
}

export interface AppSummary {
  id: number;
  name: string;
  runtime: Runtime;
  status: Status;
  source: Source;
  unit: string;
  cwd: string;
  command: string;
  pkgVersion: string;
  pids: number[];
  procs: number;
  cpu: number;
  cores: number[];
  affinity: string;
  rss: number;
  pss: number;
  threads: number;
  fds: number;
  readBps: number;
  writeBps: number;
  ports: Port[];
  sockets: string[];
  domains: Domain[];
  startedAt: number;
  restarts: number;
  firstSeen: number;
  lastSeen: number;
  sparkCpu: number[];
  sparkMem: number[];
}

export interface Process {
  pid: number;
  ppid: number;
  cmd: string;
  cpu: number;
  rss: number;
  threads: number;
  core: number;
  startedAt: number;
}

export interface AppEvent {
  ts: number;
  kind: EventKind;
  detail: string;
}

export interface GitInfo {
  branch: string;
  commit: string;
}

export interface Location {
  dir: string;
  cwd: string;
  entry: string;
  user: string;
  exe: string;
  runtimeVersion: string;
  unitFile: string;
  packageManager: PackageManager | string;
  gitRemote: string;
  logs: string;
}

export interface Script {
  name: string;
  command: string;
}

export interface Control {
  enabled: boolean;
  actions: Action[];
  jobs: boolean;
  reason: string;
}

export interface Job {
  id: string;
  appId: number;
  kind: 'install' | 'run';
  script: string;
  command: string;
  user: string;
  dir: string;
  status: JobStatus;
  exitCode: number | null;
  startedAt: number;
  finishedAt: number;
  /** only present in GET /api/jobs/{id} */
  output?: string;
  truncated: boolean;
}

export interface AppDetail extends AppSummary {
  processes: Process[];
  events: AppEvent[];
  git: GitInfo | null;
  /** v0.2 additions; null / defaults when talking to an older backend */
  location: Location | null;
  scripts: Script[];
  control: Control;
  jobs: Job[];
}

export interface DomainRoute {
  id: string;
  hosts: string[];
  path: string;
  tls: boolean;
  source: 'openlitespeed' | 'nginx' | 'caddy' | string;
  port: number;
  socket: string;
  appId: number | null;
  appName: string;
  static: boolean;
  editable: boolean;
  reason: string;
}

export interface MutationResult {
  ok: boolean;
  output: string;
}

export interface Overview {
  host: Host;
  tracker: Tracker;
  apps: AppSummary[];
}

export interface History {
  range: Range;
  step: number;
  ts: number[];
  cpu: (number | null)[];
  cpuMax: (number | null)[];
  mem: (number | null)[];
  memMax: (number | null)[];
}

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string
  ) {
    super(message);
  }
}

// Go encodes nil slices as `null`; normalise so components can rely on arrays.
const arr = <T>(v: T[] | null | undefined): T[] => (Array.isArray(v) ? v : []);

export function normalizeApp<T extends AppSummary>(a: T): T {
  return {
    ...a,
    pids: arr(a.pids),
    cores: arr(a.cores),
    ports: arr(a.ports),
    sockets: arr(a.sockets),
    domains: arr(a.domains),
    sparkCpu: arr(a.sparkCpu),
    sparkMem: arr(a.sparkMem)
  };
}

export function normalizeOverview(o: Overview): Overview {
  return {
    host: { ...o.host, load: (Array.isArray(o.host?.load) ? o.host.load : [0, 0, 0]) as Host['load'] },
    tracker: o.tracker,
    apps: arr(o.apps).map(normalizeApp)
  };
}

async function request<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, { ...init, headers: { accept: 'application/json', ...init?.headers } });
  if (!res.ok) {
    let msg = `${res.status} ${res.statusText}`.trim();
    try {
      const text = (await res.text()).trim();
      if (text) {
        let parsed: unknown = null;
        try {
          parsed = JSON.parse(text);
        } catch {
          /* not JSON */
        }
        const err = parsed && typeof parsed === 'object' ? (parsed as { error?: unknown }).error : undefined;
        msg = typeof err === 'string' && err ? err : text.length > 400 ? text.slice(0, 400) + '…' : text;
      }
    } catch {
      /* ignore */
    }
    throw new ApiError(res.status, msg);
  }
  if (res.status === 204) return undefined as T;
  const text = await res.text();
  return (text ? JSON.parse(text) : undefined) as T;
}

/**
 * Every mutating request goes through here: the backend's CSRF guard requires
 * `X-Nodetrack: 1`, and bodies are always JSON (an empty object when none).
 */
function mutate<T>(method: 'POST' | 'DELETE', url: string, body: object = {}): Promise<T> {
  return request<T>(url, {
    method,
    headers: { 'Content-Type': 'application/json', 'X-Nodetrack': '1' },
    body: JSON.stringify(body)
  });
}

export async function getOverview(signal?: AbortSignal): Promise<Overview> {
  return normalizeOverview(await request<Overview>('api/overview', { signal }));
}

const NO_CONTROL: Control = { enabled: false, actions: [], jobs: false, reason: '' };

export async function getApp(id: number, signal?: AbortSignal): Promise<AppDetail> {
  const d = await request<AppDetail>(`api/apps/${id}`, { signal });
  const control = d.control ? { ...NO_CONTROL, ...d.control, actions: arr(d.control.actions) } : NO_CONTROL;
  return {
    ...normalizeApp(d),
    processes: arr(d.processes),
    events: arr(d.events),
    git: d.git ?? null,
    location: d.location ?? null,
    scripts: arr(d.scripts),
    control,
    jobs: arr(d.jobs)
  };
}

export async function getHistory(id: number, range: Range, signal?: AbortSignal): Promise<History> {
  const h = await request<History>(`api/apps/${id}/history?range=${range}`, { signal });
  return {
    range: h.range ?? range,
    step: h.step ?? 0,
    ts: arr(h.ts),
    cpu: arr(h.cpu),
    cpuMax: arr(h.cpuMax),
    mem: arr(h.mem),
    memMax: arr(h.memMax)
  };
}

export async function forgetApp(id: number): Promise<void> {
  await mutate<void>('DELETE', `api/apps/${id}`);
}

/** Start / stop / restart. Resolves after the command finishes (up to 90s). */
export function appAction(id: number, action: Action): Promise<MutationResult> {
  return mutate<MutationResult>('POST', `api/apps/${id}/action`, { action });
}

export function startJob(id: number, job: { kind: 'install' } | { kind: 'run'; script: string }): Promise<Job> {
  return mutate<Job>('POST', `api/apps/${id}/jobs`, job);
}

export function getJob(jobId: string, signal?: AbortSignal): Promise<Job> {
  return request<Job>(`api/jobs/${encodeURIComponent(jobId)}`, { signal });
}

export function cancelJob(jobId: string): Promise<Job> {
  return mutate<Job>('POST', `api/jobs/${encodeURIComponent(jobId)}/cancel`);
}

export const jobStreamUrl = (jobId: string) => `api/jobs/${encodeURIComponent(jobId)}/stream`;

export async function getDomains(signal?: AbortSignal): Promise<DomainRoute[]> {
  const list = await request<DomainRoute[] | null>('api/domains', { signal });
  return arr(list).map((d) => ({ ...d, hosts: arr(d.hosts) }));
}

/** Point a domain route at this app (waits for the web server reload, up to 60s). */
export function pointDomain(appId: number, domain: string, port?: number): Promise<MutationResult> {
  return mutate<MutationResult>('POST', `api/apps/${appId}/domains`, port ? { domain, port } : { domain });
}

/** Command used to install dependencies with a given package manager. */
export function installCommand(pm: string): string {
  return `${pm || 'npm'} install`;
}

/** Command used to run a package.json script with a given package manager. */
export function runCommand(pm: string, script: string): string {
  return pm === 'yarn' ? `yarn ${script}` : `${pm || 'npm'} run ${script}`;
}

/** Unique TCP port numbers an app listens on, in order. */
export function tcpPorts(ports: Port[]): number[] {
  return [...new Set(ports.map((p) => p.port))];
}

export const STREAM_URL = 'api/stream';

/** Memory figure to show: PSS when readable, else RSS. */
export const memOf = (a: Pick<AppSummary, 'pss' | 'rss'>): number => (a.pss > 0 ? a.pss : a.rss);
export const memKind = (a: Pick<AppSummary, 'pss'>): 'PSS' | 'RSS' => (a.pss > 0 ? 'PSS' : 'RSS');

export function domainUrl(d: Domain): string {
  const path = d.path && d.path !== '/' ? (d.path.startsWith('/') ? d.path : '/' + d.path) : '';
  return `${d.tls ? 'https' : 'http'}://${d.host}${path}`;
}

const LOOPBACK = new Set(['127.0.0.1', '::1', 'localhost', '[::1]']);
export const isLoopback = (addr: string) => LOOPBACK.has(addr) || addr.startsWith('127.');
export const isWildcard = (addr: string) => addr === '' || addr === '0.0.0.0' || addr === '::' || addr === '[::]' || addr === '*';
