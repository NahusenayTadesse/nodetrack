# nodetrack HTTP API

All endpoints are JSON. Sizes are bytes, times are unix seconds, CPU is percent
of ONE core (like `top`; 250 = 2.5 cores busy). Arrays are never `null`
(empty is `[]`); the only nullable field is `git`. Errors are
`{"error": "message"}` with 400 (bad id/range), 404 (unknown app) or 409.
`port.addr` is the bound IP as text: `0.0.0.0` / `::` mean all interfaces.

## GET /api/overview

```json
{
  "host": {
    "hostname": "vps-1",
    "cpus": 4,
    "cpuPct": 12.5,            // whole-host busy %, 0-100
    "memTotal": 4113264640,
    "memAvailable": 2113264640,
    "load": [0.12, 0.2, 0.18],
    "uptime": 123456
  },
  "tracker": { "rss": 9437184, "interval": 5, "version": "0.1.0" },
  "apps": [AppSummary]
}
```

### AppSummary

```json
{
  "id": 3,
  "name": "shop-api",               // pm2 name > systemd unit > package.json name > dir name
  "runtime": "node",                // "node" | "bun"
  "status": "up",                   // "up" | "down"
  "source": "pm2",                  // "pm2" | "systemd" | "manual"
  "unit": "shop-api",               // pm2 name or systemd unit, "" if none
  "cwd": "/srv/shop-api",
  "command": "node dist/server.js",
  "pkgVersion": "1.4.0",            // from package.json, "" if none
  "pids": [1234, 1240],
  "procs": 2,
  "cpu": 3.2,
  "cores": [0, 2],                  // CPUs the processes last ran on
  "affinity": "0-3",
  "rss": 104857600,
  "pss": 90000000,                  // 0 when not readable
  "threads": 22,
  "fds": 41,
  "readBps": 0,
  "writeBps": 2048,
  "ports": [{ "port": 3000, "addr": "127.0.0.1" }],
  "sockets": ["/run/shop.sock"],
  "domains": [{ "host": "shop.example.com", "path": "/", "source": "nginx", "tls": true }],
  "startedAt": 1761830000,          // 0 when down
  "restarts": 2,                    // restarts seen in last 24h
  "firstSeen": 1761000000,
  "lastSeen": 1761830500,
  "sparkCpu": [1.2, 3.4],           // last <=60 samples, oldest first
  "sparkMem": [100000000, 101000000]
}
```

## GET /api/apps/{id}

AppSummary plus:

```json
{
  "processes": [
    { "pid": 1234, "ppid": 1, "cmd": "node dist/server.js", "cpu": 2.1,
      "rss": 80000000, "threads": 11, "core": 2, "startedAt": 1761830000 }
  ],
  "events": [ { "ts": 1761830000, "kind": "start", "detail": "pid 1234" } ],
  "git": { "branch": "main", "commit": "a1b2c3d" }   // null when not a git repo
}
```

Event kinds: `discovered`, `start`, `stop`, `restart`.

## GET /api/apps/{id}/history?range=1h|6h|24h|7d|30d

Columnar (ready for uPlot). `mem` is PSS when available, else RSS. Values are
never null; missing time is simply absent. `cpu`/`mem` are bucket averages and
`cpuMax`/`memMax` the peak within the bucket (raw 5s samples for `1h`, 1-minute
buckets for `6h`/`24h`, 1-hour buckets for `7d`/`30d`). Unknown range → 400.

```json
{ "range": "1h", "step": 5,
  "ts": [1761830000, 1761830005],
  "cpu": [1.2, 3.4], "cpuMax": [1.2, 3.4],
  "mem": [100000000, 101000000], "memMax": [100000000, 101000000] }
```

## DELETE /api/apps/{id}

Forget an app (only allowed when status is `down`). 204 on success, 409 if up.

## GET /api/stream

Server-Sent Events. Event name `overview`, data = same body as `/api/overview`,
pushed every collection tick.
