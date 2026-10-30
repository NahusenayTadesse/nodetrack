package procfs

import (
	"bytes"
	"strconv"
)

// CPUTimes are aggregate host CPU counters from the first line of /proc/stat.
type CPUTimes struct {
	Total, Idle uint64
}

// HostCPU returns aggregate CPU counters and the number of online CPUs.
func (r *Reader) HostCPU() (CPUTimes, int, error) {
	var t CPUTimes
	b, err := r.read(Root + "/stat")
	if err != nil {
		return t, 0, err
	}
	cpus := 0
	for line := range bytes.SplitSeq(b, []byte{'\n'}) {
		if !bytes.HasPrefix(line, []byte("cpu")) {
			continue
		}
		if len(line) > 3 && line[3] != ' ' {
			cpus++
			continue
		}
		// cpu  user nice system idle iowait irq softirq steal guest guest_nice
		fs := bytes.Fields(line[3:])
		for i, f := range fs {
			if i >= 8 { // guest time is already included in user/nice
				break
			}
			v := atou(f)
			t.Total += v
			if i == 3 || i == 4 {
				t.Idle += v
			}
		}
	}
	return t, cpus, nil
}

// BootTime returns the boot time as unix seconds.
func (r *Reader) BootTime() (int64, error) {
	b, err := r.read(Root + "/stat")
	if err != nil {
		return 0, err
	}
	v, ok := field(b, "btime")
	if !ok {
		return 0, errFormat
	}
	return int64(atou(v)), nil
}

// MemInfo returns MemTotal and MemAvailable in bytes.
func (r *Reader) MemInfo() (total, avail int64, err error) {
	b, err := r.read(Root + "/meminfo")
	if err != nil {
		return 0, 0, err
	}
	t, _ := field(b, "MemTotal:")
	a, _ := field(b, "MemAvailable:")
	return int64(atou(t)) * 1024, int64(atou(a)) * 1024, nil
}

func (r *Reader) LoadAvg() ([3]float64, error) {
	var l [3]float64
	b, err := r.read(Root + "/loadavg")
	if err != nil {
		return l, err
	}
	fs := bytes.Fields(b)
	for i := 0; i < 3 && i < len(fs); i++ {
		l[i], _ = strconv.ParseFloat(string(fs[i]), 64)
	}
	return l, nil
}

func (r *Reader) Uptime() (int64, error) {
	b, err := r.read(Root + "/uptime")
	if err != nil {
		return 0, err
	}
	return int64(atou(b)), nil
}

// SelfRSS returns this process's resident set size in bytes.
func (r *Reader) SelfRSS() int64 {
	b, err := r.read(Root + "/self/status")
	if err != nil {
		return 0
	}
	v, _ := field(b, "VmRSS:")
	return int64(atou(v)) * 1024
}
