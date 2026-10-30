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

---

## Controls (v0.2)

Controls are **off** unless nodetrack runs with `-controls` **and** basic auth
is configured. `tracker.controls` in `/api/overview` says whether they're on.

Every mutating request (POST/DELETE) must send the header `X-Nodetrack: 1`
(CSRF guard: browsers can't add it cross-site without a CORS preflight, which
is never granted) and a JSON body where noted. Missing header → 403.

### Additions to GET /api/apps/{id}

```json
{
  "location": {
    "dir": "/home/admin/apps/shop",        // package root (has package.json); falls back to cwd
    "cwd": "/home/admin/apps/shop",
    "entry": "/home/admin/apps/shop/server.js",   // absolute entry file, "" if unknown
    "user": "admin",                        // owner of the app's files; jobs run as this user
    "exe": "/home/admin/.nvm/versions/node/v24.19.0/bin/node",
    "runtimeVersion": "v24.19.0",           // "" if unknown
    "unitFile": "/etc/systemd/system/shop.service",  // "" if not systemd
    "packageManager": "npm",                // npm | pnpm | yarn | bun
    "gitRemote": "https://github.com/me/shop",       // credentials stripped, "" if none
    "logs": "journalctl -u shop.service -f" // how to read logs on the server, "" if unknown
  },
  "scripts": [{ "name": "build", "command": "vite build" }],   // package.json scripts by name
  "control": {
    "enabled": true,                        // controls on for this server
    "actions": ["stop", "restart"],         // allowed right now (depends on source + status)
    "jobs": true,                           // install/run allowed
    "reason": ""                            // human-readable why something is unavailable
  },
  "jobs": [Job]                             // last 10 for this app, newest first, no output
}
```

`actions` by source: systemd → start (when down), stop/restart (when up);
pm2 → same; manual → stop only (nothing can start it again).

New event kinds: `action` (e.g. "restart: ok") and `job` (e.g. "npm run build: exit 0, 12s").

### Job

```json
{ "id": "9f2c41d0", "appId": 3, "kind": "install", "script": "",
  "command": "npm install", "user": "admin", "dir": "/home/admin/apps/shop",
  "status": "running",          // running | ok | failed | canceled
  "exitCode": null,             // number once finished
  "startedAt": 1761830000, "finishedAt": 0,
  "output": "...",              // only in GET /api/jobs/{id}
  "truncated": false }          // true when early output was dropped (keeps last 512 KiB)
```

### POST /api/apps/{id}/action

Body `{"action": "start" | "stop" | "restart"}`. Waits for the command (up to
90s). 200 `{"ok": true, "output": "..."}`; 422 `{"error": "..."}` when the
action isn't allowed or the command failed (the error includes its output).
The app's status changes on the next tick (≤5s).

### POST /api/apps/{id}/jobs

Body `{"kind": "install"}` or `{"kind": "run", "script": "build"}`.
202 → Job. 409 when this app already has a running job or the server is at its
job limit (2). 422 when the script doesn't exist.

### GET /api/jobs/{id}

Job including `output`.

### GET /api/jobs/{id}/stream

SSE. First an `output` event with everything so far, then `output` events as
text arrives (data is a JSON string), then one `done` event whose data is the
final Job (without output), then the stream closes. A finished job sends its
output and `done` immediately.

### POST /api/jobs/{id}/cancel

Stops a running job. 200 → Job.

### GET /api/domains

Every domain route the reverse proxies know about, for the "point a domain at
this app" picker. Available whether or not controls are on (`editable` is
false when they're off).

```json
[{
  "id": "shop.example.com#shopapp",      // opaque, pass back as-is
  "hosts": ["shop.example.com", "www.shop.example.com"],
  "path": "/",
  "tls": true,
  "source": "openlitespeed",              // openlitespeed | nginx | caddy
  "port": 3000,                           // current target port, 0 if socket/none
  "socket": "",
  "appId": 3,                             // app currently behind it, null if none
  "appName": "shop",
  "static": false,                        // true: site serves files today, no proxy yet
  "editable": true,
  "reason": ""                            // why not editable
}]
```

### POST /api/apps/{id}/domains

Body `{"domain": "<id from /api/domains>", "port": 3040}` (`port` optional,
defaults to the app's first TCP port). Points that domain route at this app.
For a `static` site it adds a proxy in front of the site's files. Waits for
the web server's graceful restart (up to 60s). 200 `{"ok": true, "output": "..."}`,
422 `{"error": "..."}`. Domains update in the UI within ~15s. Recorded as an
`action` event on the app.
