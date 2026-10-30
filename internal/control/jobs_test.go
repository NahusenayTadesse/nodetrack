package control

import (
	"strings"
	"testing"
)

func TestJobOutputOffsets(t *testing.T) {
	j := newJob(1, "run", "build", "npm run build", "u", "/x")
	j.Write([]byte("hello "))
	data, off, _ := j.ReadFrom(0)
	if data != "hello " || off != 6 {
		t.Fatalf("got %q %d", data, off)
	}
	j.Write([]byte("world"))
	if data, off, _ = j.ReadFrom(off); data != "world" || off != 11 {
		t.Fatalf("incremental read: %q %d", data, off)
	}
	// Overflow: a reader that fell behind resumes at the oldest kept byte.
	j.Write([]byte(strings.Repeat("x", jobOutputLimit)))
	data, _, _ = j.ReadFrom(off)
	if len(data) != jobOutputLimit || !j.View(false).Truncated {
		t.Fatalf("after overflow: len %d truncated %v", len(data), j.View(false).Truncated)
	}
	j.finish(0, nil)
	select {
	case <-j.Done():
	default:
		t.Fatal("done not closed")
	}
	if v := j.View(false); v.Status != "ok" || v.ExitCode == nil || *v.ExitCode != 0 || v.Output != "" {
		t.Fatalf("final view: %+v", v)
	}
}
