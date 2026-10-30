package control

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"
)

// spec describes a command to run on behalf of an app.
type spec struct {
	Name string // transient unit suffix, unique per run
	Root bool   // run as root (privileged helper); otherwise as UID/GID
	UID  int
	GID  int
	Dir  string
	Env  []string // KEY=VALUE, the whole environment
	Argv []string
}

// runner starts commands. As root it uses systemd-run, so each command gets
// its own transient unit: it runs as the target user, outside nodetrack's
// read-only sandbox and memory limit, and can be stopped as a whole.
// Otherwise (development) it runs commands directly as the current user.
type runner struct {
	systemd bool
}

func newRunner() runner {
	_, err := exec.LookPath("systemd-run")
	return runner{systemd: os.Geteuid() == 0 && err == nil}
}

type proc struct {
	cmd  *exec.Cmd
	unit string
}

func (r runner) start(s spec, out io.Writer) (*proc, error) {
	var cmd *exec.Cmd
	p := &proc{}
	if r.systemd {
		p.unit = "nodetrack-" + s.Name + ".service"
		args := []string{"--quiet", "--wait", "--pipe", "--collect", "--service-type=exec",
			"--unit=" + p.unit}
		if !s.Root {
			args = append(args, "--uid="+strconv.Itoa(s.UID), "--gid="+strconv.Itoa(s.GID))
		}
		if s.Dir != "" {
			args = append(args, "--working-directory="+s.Dir)
		}
		for _, e := range s.Env {
			args = append(args, "--setenv="+e)
		}
		args = append(args, "--")
		cmd = exec.Command("systemd-run", append(args, s.Argv...)...)
	} else {
		if !s.Root && s.UID != os.Getuid() {
			return nil, fmt.Errorf("can't run as uid %d: nodetrack isn't running as root", s.UID)
		}
		cmd = exec.Command(s.Argv[0], s.Argv[1:]...)
		cmd.Dir = s.Dir
		cmd.Env = s.Env
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	p.cmd = cmd
	return p, nil
}

// wait returns the command's exit code (-1 if it died without one).
func (p *proc) wait() (int, error) {
	err := p.cmd.Wait()
	var ee *exec.ExitError
	switch {
	case err == nil:
		return 0, nil
	case errors.As(err, &ee):
		return ee.ExitCode(), nil
	default:
		return -1, err
	}
}

// stop terminates the command and everything it started.
func (p *proc) stop() {
	if p.unit != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		exec.CommandContext(ctx, "systemctl", "stop", p.unit).Run()
		return
	}
	if p.cmd.Process != nil {
		syscall.Kill(-p.cmd.Process.Pid, syscall.SIGTERM)
		time.AfterFunc(10*time.Second, func() { syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL) })
	}
}

// run starts a command and waits for it, killing it after timeout.
func (r runner) run(s spec, timeout time.Duration) (string, int, error) {
	var buf capBuffer
	buf.limit = 64 << 10
	p, err := r.start(s, &buf)
	if err != nil {
		return "", -1, err
	}
	t := time.AfterFunc(timeout, p.stop)
	code, err := p.wait()
	t.Stop()
	return buf.String(), code, err
}
