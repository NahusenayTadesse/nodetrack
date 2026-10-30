package proxy

import (
	"os"
	"path/filepath"
	"testing"

	"nodetrack/internal/procfs"
)

func TestNginxRoutes(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		p := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("nginx.conf", `
user www-data;
events { worker_connections 768; }
http {
    include mime.types;
    upstream shop_backend {
        server 127.0.0.1:3001 weight=5;
        server unix:/run/shop.sock;
    }
    include sites-enabled/*;
}`)
	write("mime.types", `types { text/html html; }`)
	write("sites-enabled/blog.conf", `
server {
    listen 443 ssl http2;
    server_name blog.example.com www.blog.example.com;
    location / { proxy_pass http://localhost:3000; }
    location ~* \.(png|jpg)$ { root /var/www; }
    location /api/ {
        if ($request_method = POST) { proxy_pass http://127.0.0.1:4000/; }
    }
}
server {
    listen 80;
    server_name _;
    return 301 https://$host$request_uri; # "quoted ; in comment"
}`)
	write("sites-enabled/shop.conf", `
server {
    listen 80;
    server_name "shop.example.com";
    set $up http://[::1]:5000;
    location / { proxy_pass http://shop_backend; }
    location /v2 { proxy_pass $up; }
    location /dyn { proxy_pass http://$backend:9; }
}`)

	routes, err := NginxRoutes(filepath.Join(dir, "nginx.conf"))
	if err != nil {
		t.Fatal(err)
	}
	type want struct {
		host, path, upHost, socket string
		port                       int
		tls                        bool
	}
	wants := []want{
		{"blog.example.com", "/", "localhost", "", 3000, true},
		{"www.blog.example.com", "/", "localhost", "", 3000, true},
		{"blog.example.com", "/api/", "127.0.0.1", "", 4000, true},
		{"www.blog.example.com", "/api/", "127.0.0.1", "", 4000, true},
		{"shop.example.com", "/", "127.0.0.1", "", 3001, false},
		{"shop.example.com", "/", "", "/run/shop.sock", 0, false},
		{"shop.example.com", "/v2", "::1", "", 5000, false},
	}
	if len(routes) != len(wants) {
		t.Fatalf("got %d routes, want %d: %+v", len(routes), len(wants), routes)
	}
	for _, w := range wants {
		found := false
		for _, r := range routes {
			if r.Host == w.host && r.Path == w.path && r.UpHost == w.upHost &&
				r.Socket == w.socket && r.UpPort == w.port && r.TLS == w.tls {
				found = true
			}
		}
		if !found {
			t.Errorf("missing route %+v in %+v", w, routes)
		}
	}

	res := &Resolver{localIPs: map[string]bool{"localhost": true}}
	res.nginx = routes
	got := res.Match([]procfs.Listener{{Addr: "0.0.0.0", Port: 3001}}, nil)
	if len(got) != 1 || got[0].Host != "shop.example.com" {
		t.Errorf("match by port: %+v", got)
	}
	got = res.Match(nil, []string{"/run/shop.sock"})
	if len(got) != 1 || got[0].Host != "shop.example.com" {
		t.Errorf("match by socket: %+v", got)
	}
	// Bound to loopback: reachable through "localhost" upstreams.
	got = res.Match([]procfs.Listener{{Addr: "127.0.0.1", Port: 3000}}, nil)
	if len(got) != 2 {
		t.Errorf("match blog: %+v", got)
	}
	// A proxy to a remote host on the same port is not this app.
	res.nginx = append(res.nginx, Route{Domain: Domain{Host: "remote.example.com"}, UpHost: "10.9.9.9", UpPort: 3000})
	got = res.Match([]procfs.Listener{{Addr: "0.0.0.0", Port: 3000}}, nil)
	for _, d := range got {
		if d.Host == "remote.example.com" {
			t.Errorf("remote upstream matched local app")
		}
	}
}
