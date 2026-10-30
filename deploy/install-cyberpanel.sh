#!/usr/bin/env bash
# Installs nodetrack as a root systemd service and publishes it on a
# CyberPanel/OpenLiteSpeed site with HTTPS and basic auth.
#
#   sudo bash install-cyberpanel.sh <domain> [path/to/nodetrack-binary]
#
# Safe to re-run: every step checks before it changes anything, edited files
# are backed up, and the OpenLiteSpeed change is rolled back if the server
# doesn't come back up.
set -euo pipefail

DOMAIN=${1:?usage: sudo bash $0 <domain> [binary]}
BIN_SRC=${2:-$(dirname "$(readlink -f "$0")")/nodetrack}
PORT=9100
LSWS=/usr/local/lsws
VHCONF=$LSWS/conf/vhosts/$DOMAIN/vhost.conf
DOCROOT=/home/$DOMAIN/public_html
TS=$(date +%Y%m%d-%H%M%S)

say()  { printf '\n\033[1;32m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m!!\033[0m %s\n' "$*"; }
die()  { printf '\033[1;31mxx\033[0m %s\n' "$*" >&2; exit 1; }

[ "$(id -u)" = 0 ] || die "run with sudo"
[ -x "$BIN_SRC" ] || die "binary not found: $BIN_SRC"
command -v cyberpanel >/dev/null || die "cyberpanel CLI not found"
[ -x $LSWS/bin/lswsctrl ] || die "OpenLiteSpeed not found in $LSWS"
if ss -ltnpH "sport = :$PORT" | grep -q . && ! ss -ltnpH "sport = :$PORT" | grep -q nodetrack; then
  die "port $PORT is used by something else: $(ss -ltnpH "sport = :$PORT")"
fi

# ---------------------------------------------------------------- service --
say "Installing nodetrack service"
install -m 0755 "$BIN_SRC" /usr/local/bin/nodetrack
install -d -m 0700 /var/lib/nodetrack
if [ ! -f /etc/nodetrack.env ]; then
  PASS=$(head -c 32 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c 24)
  printf 'NODETRACK_USER=admin\nNODETRACK_PASS=%s\n' "$PASS" > /etc/nodetrack.env
  chmod 600 /etc/nodetrack.env
  echo "generated dashboard credentials in /etc/nodetrack.env"
fi
cat > /etc/systemd/system/nodetrack.service <<EOF
[Unit]
Description=nodetrack - Node.js/Bun app monitor
After=network.target

[Service]
# Root because /proc is mounted hidepid=invisible and the OpenLiteSpeed
# configs are root-only. The service itself only reads.
# -controls: start/stop/restart, package scripts and domain changes from the
# dashboard. Scripts run as the app's owner in their own transient unit; domain
# edits run as a separate root helper with backups and automatic rollback.
ExecStart=/usr/local/bin/nodetrack -listen 127.0.0.1:$PORT -db /var/lib/nodetrack/nodetrack.db -caddy "" -controls
EnvironmentFile=/etc/nodetrack.env
NoNewPrivileges=yes
ProtectSystem=strict
ReadWritePaths=/var/lib/nodetrack
ProtectHome=read-only
ProtectKernelTunables=yes
ProtectKernelModules=yes
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6 AF_NETLINK
MemoryHigh=32M
MemoryMax=64M
Restart=on-failure

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable nodetrack >/dev/null 2>&1
systemctl restart nodetrack
for _ in $(seq 20); do
  code=$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$PORT/" || true)
  [ "$code" = 401 ] && break
  sleep 0.5
done
[ "$code" = 401 ] || { journalctl -u nodetrack -n 20 --no-pager; die "nodetrack did not come up (HTTP $code)"; }
echo "nodetrack is running on 127.0.0.1:$PORT (auth required)"

# ---------------------------------------------------------------- website --
if [ ! -f "$VHCONF" ]; then
  say "Creating CyberPanel website $DOMAIN"
  # PHP is irrelevant for a proxied site; pick a version every CyberPanel CLI knows.
  PHPV=""
  for v in 82 81 83 80 74; do [ -d "$LSWS/lsphp$v" ] && { PHPV="${v:0:1}.${v:1}"; break; }; done
  [ -n "$PHPV" ] || die "no lsphp found to satisfy cyberpanel createWebsite"
  out=$(cyberpanel createWebsite --package Default --owner admin --domainName "$DOMAIN" \
        --email "webmaster@$DOMAIN" --php "$PHPV" 2>&1) || true
  echo "$out"
  [ -f "$VHCONF" ] || die "website creation failed; create $DOMAIN in the CyberPanel UI and re-run"
else
  say "Website $DOMAIN already exists"
fi

# -------------------------------------------------------------------- ssl --
if [ ! -f "/etc/letsencrypt/live/$DOMAIN/fullchain.pem" ]; then
  say "Issuing Let's Encrypt certificate"
  cyberpanel issueSSL --domainName "$DOMAIN" || warn "issueSSL failed; the site will work over http only until you issue SSL in CyberPanel"
else
  say "Certificate already present"
fi

# ------------------------------------------------------------ proxy setup --
say "Configuring reverse proxy $DOMAIN -> 127.0.0.1:$PORT"
grep -Eq '^\s*autoLoadHtaccess\s+1' "$VHCONF" ||
  die "$VHCONF doesn't auto-load .htaccess; add the rewrite rule in CyberPanel > Rewrite Rules instead"
changed=0
if ! grep -q 'extprocessor nodetrack' "$VHCONF"; then
  cp -a "$VHCONF" "$VHCONF.bak-nodetrack-$TS"
  cat >> "$VHCONF" <<EOF

extprocessor nodetrack {
  type                    proxy
  address                 127.0.0.1:$PORT
  maxConns                100
  pcKeepAliveTimeout      60
  initTimeout             60
  retryTimeout            0
  respBuffer              0
}
EOF
  changed=1
fi
HT=$DOCROOT/.htaccess
if ! grep -qs 'HTTP://nodetrack/' "$HT"; then
  [ -f "$HT" ] && cp -a "$HT" "$HT.bak-nodetrack-$TS"
  cat > "$HT" <<'EOF'
RewriteEngine On
# keep Let's Encrypt renewals working
RewriteCond %{REQUEST_URI} !^/\.well-known/
RewriteRule ^(.*)$ HTTP://nodetrack/$1 [P,L]
EOF
  chown --reference="$DOCROOT" "$HT"
  chmod 644 "$HT"
  changed=1
fi

if [ $changed = 1 ]; then
  say "Gracefully restarting OpenLiteSpeed"
  $LSWS/bin/lswsctrl restart >/dev/null
  sleep 3
  if ! systemctl is-active --quiet lsws; then
    warn "OpenLiteSpeed is not active, rolling back"
    [ -f "$VHCONF.bak-nodetrack-$TS" ] && cp -a "$VHCONF.bak-nodetrack-$TS" "$VHCONF"
    if [ -f "$HT.bak-nodetrack-$TS" ]; then cp -a "$HT.bak-nodetrack-$TS" "$HT"; else rm -f "$HT"; fi
    $LSWS/bin/lswsctrl restart >/dev/null
    die "proxy configuration rolled back; check $LSWS/logs/error.log"
  fi
fi

# ----------------------------------------------------------------- verify --
say "Verifying"
. /etc/nodetrack.env
scheme=https
[ -f "/etc/letsencrypt/live/$DOMAIN/fullchain.pem" ] || scheme=http
code=$(curl -sk -o /dev/null -w '%{http_code}' --resolve "$DOMAIN:443:127.0.0.1" --resolve "$DOMAIN:80:127.0.0.1" "$scheme://$DOMAIN/")
auth=$(curl -sk -o /dev/null -w '%{http_code}' -u "$NODETRACK_USER:$NODETRACK_PASS" --resolve "$DOMAIN:443:127.0.0.1" --resolve "$DOMAIN:80:127.0.0.1" "$scheme://$DOMAIN/api/overview")
sse=$(timeout 4 curl -skN -u "$NODETRACK_USER:$NODETRACK_PASS" --resolve "$DOMAIN:443:127.0.0.1" --resolve "$DOMAIN:80:127.0.0.1" "$scheme://$DOMAIN/api/stream" 2>/dev/null | head -c 20 || true)
echo "without auth: HTTP $code (want 401) | with auth: HTTP $auth (want 200) | live stream: $([ "${sse:0:6}" = event: ] && echo ok || echo 'not streaming through proxy')"

cat <<EOF

  Dashboard: $scheme://$DOMAIN/
  Username:  $NODETRACK_USER
  Password:  $NODETRACK_PASS     (stored in /etc/nodetrack.env)

  Logs:      journalctl -u nodetrack -f
  Uninstall: systemctl disable --now nodetrack; rm /etc/systemd/system/nodetrack.service
             and delete the site in CyberPanel.
EOF
