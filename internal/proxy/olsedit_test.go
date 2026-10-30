package proxy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOLSPoint(t *testing.T) {
	dir := t.TempDir()
	conf := filepath.Join(dir, "lsws/conf/httpd_config.conf")
	write := func(p, body string) {
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(conf, `
extprocessor shared { type proxy
}
extprocessor shared2 {
  type                    proxy
  address                 127.0.0.1:3002
}
virtualhost app.example.com {
  vhRoot                  `+dir+`/home/app.example.com
  configFile              $SERVER_ROOT/conf/vhosts/$VH_NAME/vhost.conf
}
virtualhost new.example.com {
  vhRoot                  `+dir+`/home/new.example.com
  configFile              $SERVER_ROOT/conf/vhosts/$VH_NAME/vhost.conf
}
virtualhost shared.example.com {
  vhRoot                  `+dir+`/home/shared.example.com
  configFile              $SERVER_ROOT/conf/vhosts/$VH_NAME/vhost.conf
}
listener Default {
  address                 *:80
  map                     app.example.com app.example.com
  map                     new.example.com new.example.com
  map                     shared.example.com shared.example.com
}
`)
	appConf := filepath.Join(dir, "lsws/conf/vhosts/app.example.com/vhost.conf")
	original := `docRoot $VH_ROOT/public_html
extprocessor appbe {
  type                    proxy
  address                 127.0.0.1:3000
  respBuffer              0
}
rewrite  {
  enable                  1
  autoLoadHtaccess        1
}
`
	write(appConf, original)
	write(filepath.Join(dir, "lsws/conf/vhosts/new.example.com/vhost.conf"), `docRoot $VH_ROOT/public_html
rewrite  {
  enable                  1
  autoLoadHtaccess        1
}
`)
	write(filepath.Join(dir, "home/new.example.com/public_html/index.html"), "hi")
	write(filepath.Join(dir, "home/new.example.com/public_html/.htaccess"), "Options -Indexes\n")
	write(filepath.Join(dir, "lsws/conf/vhosts/shared.example.com/vhost.conf"), `docRoot $VH_ROOT/public_html
rewrite  {
  enable 1
  rules <<<END_rules
RewriteRule ^(.*)$ HTTP://shared2/$1 [P,L]
END_rules
}
`)

	// Entries reflect editability.
	r := &Resolver{}
	r.olsModel, _ = loadOLSModel(conf)
	byID := map[string]Entry{}
	for _, e := range r.Entries(nil) {
		byID[e.ID] = e
	}
	if e := byID["app.example.com#appbe"]; !e.Editable || e.Port != 3000 {
		t.Errorf("app entry: %+v", e)
	}
	if e := byID["new.example.com#"]; !e.Static || !e.Editable {
		t.Errorf("static entry: %+v", e)
	}
	if e := byID["shared.example.com#shared2"]; e.Editable || !strings.Contains(e.Reason, "shared") {
		t.Errorf("shared entry: %+v", e)
	}

	// Repoint and restore.
	restore, summary, err := OLSPoint(conf, "app.example.com", "appbe", "127.0.0.1:3040")
	if err != nil {
		t.Fatal(err)
	}
	t.Log(summary)
	routes, _ := OLSRoutes(conf)
	found := false
	for _, rt := range routes {
		if rt.Host == "app.example.com" && rt.UpPort == 3040 {
			found = true
		}
	}
	if !found {
		t.Errorf("not repointed: %+v", routes)
	}
	if b, _ := os.ReadFile(appConf); !strings.Contains(string(b), "  address                 127.0.0.1:3040\n") {
		t.Errorf("address line formatting lost:\n%s", b)
	}
	if err := restore(); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(appConf); string(b) != original {
		t.Errorf("restore mismatch:\n%s", b)
	}

	// Static site gets a proxy in front, existing .htaccess kept below it.
	restore, _, err = OLSPoint(conf, "new.example.com", "", "127.0.0.1:3050")
	if err != nil {
		t.Fatal(err)
	}
	ht, _ := os.ReadFile(filepath.Join(dir, "home/new.example.com/public_html/.htaccess"))
	if !strings.HasPrefix(string(ht), "# added by nodetrack") || !strings.HasSuffix(string(ht), "Options -Indexes\n") {
		t.Errorf("htaccess:\n%s", ht)
	}
	routes, _ = OLSRoutes(conf)
	found = false
	for _, rt := range routes {
		if rt.Host == "new.example.com" && rt.UpPort == 3050 {
			found = true
		}
	}
	if !found {
		t.Errorf("static not proxied: %+v", routes)
	}
	restore()
	if ht, _ := os.ReadFile(filepath.Join(dir, "home/new.example.com/public_html/.htaccess")); string(ht) != "Options -Indexes\n" {
		t.Errorf("htaccess not restored: %q", ht)
	}

	// Refusals.
	for _, c := range []struct{ vhost, backend, addr string }{
		{"shared.example.com", "shared2", "127.0.0.1:3040"}, // shared backend
		{"app.example.com", "appbe", "127.0.0.1:3040;\nextprocessor x"},
		{"app.example.com", "appbe", "evil.example.com:80"},
		{"nope.example.com", "", "127.0.0.1:3040"},
	} {
		if _, _, err := OLSPoint(conf, c.vhost, c.backend, c.addr); err == nil {
			t.Errorf("expected refusal for %+v", c)
		}
	}
}
