const UNITS = ['B', 'KiB', 'MiB', 'GiB', 'TiB'];

/** 1536 -> "1.5 KiB", 104857600 -> "100 MiB". */
export function bytes(n: number | null | undefined): string {
  if (n == null || !Number.isFinite(n)) return '–';
  if (n < 0) return '-' + bytes(-n);
  let i = 0;
  let v = n;
  while (v >= 1024 && i < UNITS.length - 1) {
    v /= 1024;
    i++;
  }
  if (i === 0) return `${Math.round(v)} B`;
  const digits = v >= 100 ? 0 : 1;
  return `${v.toFixed(digits)} ${UNITS[i]}`;
}

/** Split bytes into number + unit for typographic treatment. */
export function bytesParts(n: number): [string, string] {
  const s = bytes(n);
  const i = s.lastIndexOf(' ');
  return i < 0 ? [s, ''] : [s.slice(0, i), s.slice(i + 1)];
}

export function rate(bps: number): string {
  return bps > 0 ? `${bytes(bps)}/s` : '0 B/s';
}

/** Seconds -> "3d 4h", "4h 12m", "12m", "45s". */
export function duration(sec: number | null | undefined): string {
  if (sec == null || !Number.isFinite(sec) || sec < 0) return '–';
  sec = Math.floor(sec);
  const d = Math.floor(sec / 86400);
  const h = Math.floor((sec % 86400) / 3600);
  const m = Math.floor((sec % 3600) / 60);
  const s = sec % 60;
  if (d > 0) return h > 0 ? `${d}d ${h}h` : `${d}d`;
  if (h > 0) return m > 0 ? `${h}h ${m}m` : `${h}h`;
  if (m > 0) return `${m}m`;
  return `${s}s`;
}

/** Unix ts relative to now (unix seconds): "5m ago", "in 2h", "just now". */
export function relative(ts: number, now: number): string {
  if (!ts) return '–';
  const diff = now - ts;
  if (Math.abs(diff) < 10) return 'just now';
  const s = duration(Math.abs(diff));
  return diff > 0 ? `${s} ago` : `in ${s}`;
}

/** 12.345 -> "12.3%". */
export function pct(n: number | null | undefined): string {
  if (n == null || !Number.isFinite(n)) return '–';
  return `${n.toFixed(1)}%`;
}

const dtf = new Intl.DateTimeFormat(undefined, {
  year: 'numeric',
  month: 'short',
  day: 'numeric',
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit'
});

export function datetime(ts: number): string {
  return ts ? dtf.format(new Date(ts * 1000)) : '–';
}

const tf = new Intl.DateTimeFormat(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });
export function shortTime(ts: number): string {
  return ts ? tf.format(new Date(ts * 1000)) : '–';
}

/** [0,1,2,3,6] -> "0-3, 6". */
export function coreList(cores: number[]): string {
  if (!cores.length) return '–';
  const sorted = [...new Set(cores)].sort((a, b) => a - b);
  const out: string[] = [];
  let start = sorted[0];
  let prev = start;
  for (let i = 1; i <= sorted.length; i++) {
    const c = sorted[i];
    if (c === prev + 1) {
      prev = c;
      continue;
    }
    out.push(start === prev ? `${start}` : `${start}-${prev}`);
    start = prev = c;
  }
  return out.join(', ');
}

export function plural(n: number, one: string, many = one + 's'): string {
  return `${n} ${n === 1 ? one : many}`;
}
