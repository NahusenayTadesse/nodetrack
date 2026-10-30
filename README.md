# nodetrack

A tiny monitor for VPSes that run Node.js and Bun apps. It finds every app on
its own, with no agents, config or SDK, and shows RAM, CPU, cores, ports,
restarts and the public domains (from nginx/Caddy) that route to each one.

One static Go binary with the Svelte UI embedded, SQLite (WAL) for history.

**Footprint** (measured, 5s interval, 6 tracked apps, ~600 host processes):
~16 MiB RSS, of which ~5.5 MiB is private heap (the rest is shared,
reclaimable binary code); ~0.4% of one core.

## How detection works

Every tick nodetrack scans `/proc`:

1. **Runtime**: a process is node/bun if `/proc/<pid>/exe` is `node` or
   `bun`. It doesn't rely on the process name, so `process.title` and pm2
   renames don't fool it.
2. **Not an app**: package managers and runners (`npm`, `npx`, `yarn`,
   `pnpm`, `bun run <script>`, `bun install/add/x/...`, `node --run`), pm2's
   daemon, nodemon/tsx supervisors, `node -e`/REPL, editor servers (VS Code,
   Cursor) and anything in a container (other pid namespace or a
   docker/containerd/podman cgroup). Their *children* are still considered,
   which is how `npm start` → `node server.js` gets tracked.
3. **Grouping**: child node/bun processes fold into their top-most node/bun
   ancestor (cluster workers, Next.js `next-server`). Processes sharing an
   identity merge too (pm2 cluster workers).
4. **Identity**, which stays stable across restarts: pm2 name, then systemd unit,
   then runtime + working dir + entry script. The display name comes from pm2 /
   the unit / `package.json` / the directory name.
5. **Adoption**: a candidate becomes an app once it's ≥10s old and it either
   listens on a TCP port, sits behind a proxied unix socket, runs under pm2 or
   a systemd service, or matches `-include`.

Domains come from parsing `nginx.conf` (following `include` globs, upstreams,
`set $var`, unix sockets; re-parsed only when files change) and from Caddy's
admin API (`:2019/config/`, works for Caddyfile setups too).

History: raw samples for 6h, 1-minute rollups for 8 days, hourly for 400 days.
Events: `discovered`, `start`, `stop`, `restart`.

## Build and install

```sh
make            # npm ci + vite build, then a static pure-Go binary
make build-arm64
make build-cgo  # optional: C SQLite, smaller binary and ~1.5 MiB less RSS
```

```sh
scp nodetrack root@vps:/usr/local/bin/
scp deploy/nodetrack.service root@vps:/etc/systemd/system/
ssh root@vps systemctl enable --now nodetrack
```

The dashboard listens on `127.0.0.1:9100`. Put it behind your reverse proxy
(the SSE stream sets `X-Accel-Buffering: no` for nginx), and set
`NODETRACK_USER` / `NODETRACK_PASS` for basic auth.

## Flags

| flag | default | |
|---|---|---|
| `-listen` | `127.0.0.1:9100` | HTTP address |
| `-db` | `nodetrack.db` | SQLite path |
| `-interval` | `5s` | sampling period |
| `-min-age` | `10s` | ignore younger processes |
| `-nginx` | `/etc/nginx/nginx.conf` | `""` disables |
| `-caddy` | `http://127.0.0.1:2019` | `""` disables |
| `-include` / `-exclude` | | regexp over `cwd + argv` |

`GOMEMLIMIT` and `GOGC` default to 24 MiB and 50 unless set.

## Development

```sh
make dev     # backend on :9100, Vite with HMR on :5173
make check   # go vet + tests + svelte-check
```

API: [docs/API.md](docs/API.md).

## Security notes

- `environ` is read only to pick out `pm_id`, `name` and `PORT`. Nothing
  else leaves the read buffer.
- Command lines are shown in the UI, so keep the dashboard private.
