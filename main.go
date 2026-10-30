// nodetrack auto-discovers Node.js and Bun apps on a Linux host and tracks
// their CPU, memory, cores, ports and public domains.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"runtime/debug"
	"syscall"
	"time"

	"nodetrack/internal/collector"
	"nodetrack/internal/proxy"
	"nodetrack/internal/server"
	"nodetrack/internal/store"
	"nodetrack/internal/web"
)

func main() {
	var (
		listen    = flag.String("listen", "127.0.0.1:9100", "HTTP listen address")
		dbPath    = flag.String("db", "nodetrack.db", "SQLite database path")
		interval  = flag.Duration("interval", 5*time.Second, "sampling interval")
		minAge    = flag.Duration("min-age", 10*time.Second, "ignore processes younger than this")
		nginxConf = flag.String("nginx", "/etc/nginx/nginx.conf", "nginx main config (\"\" to disable)")
		olsConf   = flag.String("ols", "/usr/local/lsws/conf/httpd_config.conf", "OpenLiteSpeed/CyberPanel main config (\"\" to disable)")
		caddyAPI  = flag.String("caddy", "http://127.0.0.1:2019", "Caddy admin API (\"\" to disable)")
		include   = flag.String("include", "", "regexp: also track matching processes that have no port")
		exclude   = flag.String("exclude", "", "regexp: never track matching processes")
		version   = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()
	if *version {
		fmt.Println("nodetrack", collector.Version)
		return
	}

	// Stay small unless the operator explicitly asked for something else.
	if os.Getenv("GOMEMLIMIT") == "" {
		debug.SetMemoryLimit(24 << 20)
	}
	if os.Getenv("GOGC") == "" {
		debug.SetGCPercent(50)
	}

	cfg := collector.Config{Interval: *interval, MinAge: *minAge, PortScan: 30 * time.Second}
	var err error
	if *include != "" {
		if cfg.Include, err = regexp.Compile(*include); err != nil {
			fatal("bad -include", err)
		}
	}
	if *exclude != "" {
		if cfg.Exclude, err = regexp.Compile(*exclude); err != nil {
			fatal("bad -exclude", err)
		}
	}
	if os.Geteuid() != 0 {
		slog.Warn("not running as root: processes owned by other users will be invisible")
	}

	st, err := store.Open(*dbPath)
	if err != nil {
		fatal("open database", err)
	}
	defer st.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	px := &proxy.Resolver{NginxConf: *nginxConf, OLSConf: *olsConf, CaddyAdmin: *caddyAPI}
	go px.Run(ctx, 15*time.Second)

	col, err := collector.New(cfg, st, px)
	if err != nil {
		fatal("collector", err)
	}
	go col.Run(ctx)

	user, pass := os.Getenv("NODETRACK_USER"), os.Getenv("NODETRACK_PASS")
	srv := &http.Server{
		Addr:              *listen,
		Handler:           (&server.Server{C: col, UI: web.FS(), User: user, Pass: pass}).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		// Cancel open SSE streams on shutdown instead of waiting for them.
		BaseContext: func(net.Listener) context.Context { return ctx },
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()
	slog.Info("nodetrack listening", "addr", *listen, "db", *dbPath, "auth", user != "")
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fatal("http", err)
	}
}

func fatal(msg string, err error) {
	slog.Error(msg, "err", err)
	os.Exit(1)
}
