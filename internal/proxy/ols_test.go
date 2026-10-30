package proxy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOLSRoutes(t *testing.T) {
	dir := t.TempDir()
	lsws := filepath.Join(dir, "lsws")
	home := filepath.Join(dir, "home")
	write := func(p, body string) {
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(lsws, "conf/httpd_config.conf"), `
serverName                lsws
# global processor, referenced from a vhost rewrite
extprocessor shared3002 {
  type                    proxy
  address                 127.0.0.1:3002
}
virtualhost shop.example.com {
  vhRoot                  `+home+`/shop.example.com
  configFile              $SERVER_ROOT/conf/vhosts/$VH_NAME/vhost.conf
  allowSymbolLink         1
}
virtualhost api.example.com {
  vhRoot                  `+home+`/api.example.com
  configFile              $SERVER_ROOT/conf/vhosts/$VH_NAME/vhost.conf
}
virtualhost blog.example.com {
  vhRoot                  `+home+`/blog.example.com
  configFile              $SERVER_ROOT/conf/vhosts/$VH_NAME/vhost.conf
}
listener Default {
  address                 *:80
  secure                  0
  map                     shop.example.com shop.example.com, www.shop.example.com
  map                     api.example.com api.example.com
  map                     blog.example.com blog.example.com
}
listener SSL {
  address                 *:443
  secure                  1
  map                     shop.example.com shop.example.com, www.shop.example.com
}
`)
	// CyberPanel style: processor in vhost.conf, rule in .htaccess.
	write(filepath.Join(lsws, "conf/vhosts/shop.example.com/vhost.conf"), `
docRoot                   $VH_ROOT/public_html
vhDomain                  $VH_NAME
vhAliases                 www.$VH_NAME
extprocessor shopapp {
  type                    proxy
  address                 127.0.0.1:3000
  maxConns                100
  respBuffer              0
}
rewrite  {
  enable                  1
  autoLoadHtaccess        1
}
`)
	write(filepath.Join(home, "shop.example.com/public_html/.htaccess"),
		"RewriteEngine On\nRewriteRule ^(.*)$ HTTP://shopapp/$1 [P,L]\n")
	// context proxy + heredoc rewrite to the global processor + unix socket.
	write(filepath.Join(lsws, "conf/vhosts/api.example.com/vhost.conf"), `
docRoot                   $VH_ROOT/public_html
extprocessor apisock {
  type                    proxy
  address                 uds://run/api.sock
}
context /v2/ {
  type                    proxy
  handler                 apisock
}
rewrite  {
  enable                  1
  rules                   <<<END_rules
RewriteCond %{REQUEST_URI} !^/v2/
RewriteRule ^(.*)$ http://shared3002/$1 [P,L]
END_rules
}
vhssl  {
  keyFile                 /etc/letsencrypt/live/api.example.com/privkey.pem
}
`)
	// Plain PHP site: no proxy, no routes.
	write(filepath.Join(lsws, "conf/vhosts/blog.example.com/vhost.conf"), `
docRoot                   $VH_ROOT/public_html
scripthandler  {
  add                     lsapi:blog php
}
`)

	routes, err := OLSRoutes(filepath.Join(lsws, "conf/httpd_config.conf"))
	if err != nil {
		t.Fatal(err)
	}
	type want struct {
		host, path, upHost, socket string
		port                       int
		tls                        bool
	}
	wants := []want{
		{"shop.example.com", "/", "127.0.0.1", "", 3000, true},
		{"www.shop.example.com", "/", "127.0.0.1", "", 3000, true},
		{"api.example.com", "/", "127.0.0.1", "", 3002, true},
		{"api.example.com", "/v2/", "", "/run/api.sock", 0, true},
	}
	if len(routes) != len(wants) {
		t.Fatalf("got %d routes, want %d: %+v", len(routes), len(wants), routes)
	}
	for _, w := range wants {
		found := false
		for _, r := range routes {
			if r.Host == w.host && r.Path == w.path && r.UpHost == w.upHost &&
				r.Socket == w.socket && r.UpPort == w.port && r.TLS == w.tls && r.Source == "openlitespeed" {
				found = true
			}
		}
		if !found {
			t.Errorf("missing %+v in %+v", w, routes)
		}
	}
}
