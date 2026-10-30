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
	"os/exec"
	"os/signal"
	"regexp"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"nodetrack/internal/collector"
	"nodetrack/internal/control"
	"nodetrack/internal/proxy"
	"nodetrack/internal/server"
	"nodetrack/internal/store"
	"nodetrack/internal/web"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "ols-point" {
		os.Exit(olsPoint(os.Args[2:]))
	}
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
		controls  = flag.Bool("controls", false, "allow start/stop/restart, package scripts and domain changes (requires basic auth)")
		olsReload = flag.String("ols-restart", "/usr/local/lsws/bin/lswsctrl restart", "command that gracefully restarts OpenLiteSpeed")
		olsCheck  = flag.String("ols-check", "systemctl is-active --quiet lsws", "command that exits 0 when OpenLiteSpeed is healthy")
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

	user, pass := os.Getenv("NODETRACK_USER"), os.Getenv("NODETRACK_PASS")
	if *controls && (user == "" || pass == "") {
		slog.Warn("-controls ignored: set NODETRACK_USER and NODETRACK_PASS to enable them")
		*controls = false
	}
	cfg := collector.Config{Interval: *interval, MinAge: *minAge, PortScan: 30 * time.Second, Controls: *controls}
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

	self, _ := os.Executable()
	ctl := control.New(control.Config{Enabled: *controls, Self: self, OLSConf: *olsConf,
		OLSRestart: *olsReload, OLSCheck: *olsCheck}, st)

	srv := &http.Server{
		Addr:              *listen,
		Handler:           (&server.Server{C: col, Ctl: ctl, Px: px, UI: web.FS(), User: user, Pass: pass}).Handler(),
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
	slog.Info("nodetrack listening", "addr", *listen, "db", *dbPath, "auth", user != "", "controls", *controls)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fatal("http", err)
	}
}

// olsPoint is the privileged helper behind "point a domain at this app". The
// service runs it as a separate transient unit because its own filesystem is
// read-only. It edits the config (with backups), gracefully restarts
// OpenLiteSpeed, and rolls back if the server doesn't come back healthy.
func olsPoint(args []string) int {
	fs := flag.NewFlagSet("ols-point", flag.ExitOnError)
	conf := fs.String("conf", "/usr/local/lsws/conf/httpd_config.conf", "")
	vhost := fs.String("vhost", "", "")
	backend := fs.String("backend", "", "")
	addr := fs.String("addr", "", "")
	restartCmd := fs.String("restart", "/usr/local/lsws/bin/lswsctrl restart", "")
	checkCmd := fs.String("check", "systemctl is-active --quiet lsws", "")
	fs.Parse(args)

	restore, summary, err := proxy.OLSPoint(*conf, *vhost, *backend, *addr)
	if err != nil {
		fmt.Println("error:", err)
		return 1
	}
	fmt.Println(summary)
	run := func(cmd string) error {
		f := strings.Fields(cmd)
		if len(f) == 0 {
			return nil
		}
		out, err := exec.Command(f[0], f[1:]...).CombinedOutput()
		if len(out) > 0 {
			fmt.Print(string(out))
		}
		return err
	}
	if err := run(*restartCmd); err != nil {
		fmt.Println("restart failed:", err)
	} else {
		time.Sleep(3 * time.Second)
		if err = run(*checkCmd); err == nil {
			fmt.Println("web server restarted and healthy")
			return 0
		}
		fmt.Println("web server unhealthy after restart:", err)
	}
	if err := restore(); err != nil {
		fmt.Println("ROLLBACK FAILED:", err)
		return 2
	}
	run(*restartCmd)
	fmt.Println("changes rolled back")
	return 1
}

func fatal(msg string, err error) {
	slog.Error(msg, "err", err)
	os.Exit(1)
}
