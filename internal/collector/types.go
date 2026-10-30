package collector

import (
	"nodetrack/internal/procfs"
	"nodetrack/internal/proxy"
	"nodetrack/internal/store"
)

// JSON shapes served by the API; see docs/API.md.

type Host struct {
	Hostname     string     `json:"hostname"`
	CPUs         int        `json:"cpus"`
	CPUPct       float64    `json:"cpuPct"`
	MemTotal     int64      `json:"memTotal"`
	MemAvailable int64      `json:"memAvailable"`
	Load         [3]float64 `json:"load"`
	Uptime       int64      `json:"uptime"`
}

type Tracker struct {
	RSS      int64  `json:"rss"`
	Interval int    `json:"interval"`
	Version  string `json:"version"`
	Controls bool   `json:"controls"`
}

type Overview struct {
	Host    Host         `json:"host"`
	Tracker Tracker      `json:"tracker"`
	Apps    []AppSummary `json:"apps"`
}

type AppSummary struct {
	ID         int64             `json:"id"`
	Name       string            `json:"name"`
	Runtime    string            `json:"runtime"`
	Status     string            `json:"status"`
	Source     string            `json:"source"`
	Unit       string            `json:"unit"`
	Cwd        string            `json:"cwd"`
	Command    string            `json:"command"`
	PkgVersion string            `json:"pkgVersion"`
	PIDs       []int             `json:"pids"`
	Procs      int               `json:"procs"`
	CPU        float64           `json:"cpu"`
	Cores      []int             `json:"cores"`
	Affinity   string            `json:"affinity"`
	RSS        int64             `json:"rss"`
	PSS        int64             `json:"pss"`
	Threads    int               `json:"threads"`
	FDs        int               `json:"fds"`
	ReadBps    float64           `json:"readBps"`
	WriteBps   float64           `json:"writeBps"`
	Ports      []procfs.Listener `json:"ports"`
	Sockets    []string          `json:"sockets"`
	Domains    []proxy.Domain    `json:"domains"`
	StartedAt  int64             `json:"startedAt"`
	Restarts   int               `json:"restarts"`
	FirstSeen  int64             `json:"firstSeen"`
	LastSeen   int64             `json:"lastSeen"`
	SparkCPU   []float64         `json:"sparkCpu"`
	SparkMem   []int64           `json:"sparkMem"`
}

type Process struct {
	PID       int     `json:"pid"`
	PPID      int     `json:"ppid"`
	Cmd       string  `json:"cmd"`
	CPU       float64 `json:"cpu"`
	RSS       int64   `json:"rss"`
	Threads   int     `json:"threads"`
	Core      int     `json:"core"`
	StartedAt int64   `json:"startedAt"`
}

type Git struct {
	Branch string `json:"branch"`
	Commit string `json:"commit"`
}

type AppDetail struct {
	AppSummary
	Processes []Process     `json:"processes"`
	Events    []store.Event `json:"events"`
	Git       *Git          `json:"git"`
	Location  Location      `json:"location"`
	Scripts   []Script      `json:"scripts"`
}

// Target is a snapshot of an app for the control package.
type Target struct {
	ID      int64
	Name    string
	Runtime string
	Source  string // pm2 | systemd | manual
	Unit    string // pm2 name or systemd unit
	Up      bool
	RootPID int
	PIDs    []int
	UID     int // uid of the running process; -1 if never seen
	Cwd     string
	Dir     string // package root, falls back to cwd
	Entry   string
	Exe     string
	PM2Home string
	Ports   []procfs.Listener
	Sockets []string
}

type Location struct {
	Dir            string `json:"dir"`
	Cwd            string `json:"cwd"`
	Entry          string `json:"entry"`
	User           string `json:"user"`
	Exe            string `json:"exe"`
	RuntimeVersion string `json:"runtimeVersion"`
	UnitFile       string `json:"unitFile"`
	PackageManager string `json:"packageManager"`
	GitRemote      string `json:"gitRemote"`
	Logs           string `json:"logs"`
}

type Script struct {
	Name    string `json:"name"`
	Command string `json:"command"`
}
