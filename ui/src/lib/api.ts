// Types and fetch helpers for the nodetrack HTTP API (see docs/API.md).
// Sizes are bytes, times are unix seconds, CPU is percent of ONE core.

export type Runtime = 'node' | 'bun';
export type Status = 'up' | 'down';
export type Source = 'pm2' | 'systemd' | 'manual';
export type EventKind = 'discovered' | 'start' | 'stop' | 'restart';
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

export interface AppDetail extends AppSummary {
  processes: Process[];
  events: AppEvent[];
  git: GitInfo | null;
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
  const res = await fetch(url, { headers: { accept: 'application/json' }, ...init });
  if (!res.ok) {
    let msg = `${res.status} ${res.statusText}`;
    try {
      const text = (await res.text()).trim();
      if (text) msg = text.length > 200 ? text.slice(0, 200) + '…' : text;
    } catch {
      /* ignore */
    }
    throw new ApiError(res.status, msg);
  }
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

export async function getOverview(signal?: AbortSignal): Promise<Overview> {
  return normalizeOverview(await request<Overview>('api/overview', { signal }));
}

export async function getApp(id: number, signal?: AbortSignal): Promise<AppDetail> {
  const d = await request<AppDetail>(`api/apps/${id}`, { signal });
  return {
    ...normalizeApp(d),
    processes: arr(d.processes),
    events: arr(d.events),
    git: d.git ?? null
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
  await request<void>(`api/apps/${id}`, { method: 'DELETE' });
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
