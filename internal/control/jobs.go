package control

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

const jobOutputLimit = 512 << 10

// JobInfo is the serialisable state of a job.
type JobInfo struct {
	ID         string `json:"id"`
	AppID      int64  `json:"appId"`
	Kind       string `json:"kind"` // install | run
	Script     string `json:"script"`
	Command    string `json:"command"`
	User       string `json:"user"`
	Dir        string `json:"dir"`
	Status     string `json:"status"` // running | ok | failed | canceled
	ExitCode   *int   `json:"exitCode"`
	StartedAt  int64  `json:"startedAt"`
	FinishedAt int64  `json:"finishedAt"`
	Output     string `json:"output,omitempty"`
	Truncated  bool   `json:"truncated"`
}

// Job is an install or package-script run for an app.
type Job struct {
	ID    string
	AppID int64

	mu       sync.Mutex
	info     JobInfo
	buf      []byte // last jobOutputLimit bytes
	written  int64  // total bytes ever written
	changed  chan struct{}
	done     chan struct{}
	proc     *proc
	canceled bool
}

func newJob(appID int64, kind, script, command, user, dir string) *Job {
	var id [4]byte
	rand.Read(id[:])
	j := &Job{ID: hex.EncodeToString(id[:]), AppID: appID,
		changed: make(chan struct{}), done: make(chan struct{})}
	j.info = JobInfo{ID: j.ID, AppID: appID, Kind: kind, Script: script, Command: command,
		User: user, Dir: dir, Status: "running", StartedAt: time.Now().Unix()}
	return j
}

// Write implements io.Writer for the command's stdout/stderr.
func (j *Job) Write(p []byte) (int, error) {
	j.mu.Lock()
	j.buf = append(j.buf, p...)
	if len(j.buf) > jobOutputLimit {
		j.buf = append(j.buf[:0], j.buf[len(j.buf)-jobOutputLimit:]...)
		j.info.Truncated = true
	}
	j.written += int64(len(p))
	close(j.changed)
	j.changed = make(chan struct{})
	j.mu.Unlock()
	return len(p), nil
}

// ReadFrom returns output written since offset, the new offset, and a channel
// that closes when more output arrives.
func (j *Job) ReadFrom(off int64) (string, int64, <-chan struct{}) {
	j.mu.Lock()
	defer j.mu.Unlock()
	start := j.written - int64(len(j.buf)) // offset of buf[0]
	off = max(off, start)
	return string(j.buf[off-start:]), j.written, j.changed
}

// Done closes when the job has finished.
func (j *Job) Done() <-chan struct{} { return j.done }

func (j *Job) finish(code int, err error) {
	if err != nil {
		j.Write([]byte("\nnodetrack: " + err.Error() + "\n"))
	}
	j.mu.Lock()
	j.info.FinishedAt = time.Now().Unix()
	switch {
	case j.canceled:
		j.info.Status = "canceled"
	case err != nil || code != 0:
		j.info.Status = "failed"
	default:
		j.info.Status = "ok"
	}
	if err == nil {
		j.info.ExitCode = &code
	}
	close(j.changed)
	j.changed = make(chan struct{})
	j.mu.Unlock()
	close(j.done)
}

// View returns the job's state, with or without output.
func (j *Job) View(withOutput bool) JobInfo {
	j.mu.Lock()
	defer j.mu.Unlock()
	v := j.info
	if withOutput {
		v.Output = string(j.buf)
	}
	return v
}

// capBuffer keeps the last limit bytes written to it.
type capBuffer struct {
	mu    sync.Mutex
	b     []byte
	limit int
}

func (c *capBuffer) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.b = append(c.b, p...)
	if len(c.b) > c.limit {
		c.b = c.b[len(c.b)-c.limit:]
	}
	return len(p), nil
}

func (c *capBuffer) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return string(c.b)
}
