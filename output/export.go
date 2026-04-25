package output

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"cpu_bench_go/results"
	"cpu_bench_go/runner"
	"cpu_bench_go/scoring"
)

type Document struct {
	Meta   Meta                    `json:"meta"`
	Config Config                  `json:"config"`
	Scores Scores                  `json:"scores"`
	Raw    map[string]RawBenchmark `json:"raw"`
}

type Meta struct {
	ToolVersion string `json:"tool_version"`
	Timestamp   string `json:"timestamp"`
	GoVersion   string `json:"go_version"`
	CPUModel    string `json:"cpu_model"`
	CPUCores    int    `json:"cpu_cores"`
	CPUThreads  int    `json:"cpu_threads"`
	OS          string `json:"os"`
	Arch        string `json:"arch"`
}

type Config struct {
	DurationS float64            `json:"duration_s"`
	WarmupS   float64            `json:"warmup_s"`
	Size      string             `json:"size"`
	Profile   string             `json:"profile"`
	Weights   map[string]float64 `json:"weights"`
}

type Scores struct {
	SingleThread ModeScore `json:"single_thread"`
	MultiThread  ModeScore `json:"multi_thread"`
}

type ModeScore struct {
	Integer    float64 `json:"integer"`
	Memory     float64 `json:"memory"`
	Throughput float64 `json:"throughput"`
	Latency    float64 `json:"latency"`
	Total      float64 `json:"total"`
	Label      string  `json:"label"`
}

type RawBenchmark struct {
	OpsSec     float64 `json:"ops_sec"`
	MedianNs   int64   `json:"median_ns"`
	P95Ns      int64   `json:"p95_ns"`
	P99Ns      int64   `json:"p99_ns"`
	Iterations int     `json:"iterations"`
}

func BuildDocument(
	toolVersion, size string,
	cfg runner.Config,
	score *scoring.Result,
	stResults, mtResults []*results.RawResult,
) *Document {
	// NumCPU returns logical CPUs; distinguishing physical cores from SMT
	// threads portably requires cgo or OS-specific probing, deferred.
	logicalCPUs := runtime.NumCPU()

	raw := make(map[string]RawBenchmark, len(stResults)+len(mtResults))
	for _, r := range stResults {
		if r == nil {
			continue
		}
		raw[r.Name+"_st"] = toRaw(r)
	}
	for _, r := range mtResults {
		if r == nil {
			continue
		}
		raw[r.Name+"_mt"] = toRaw(r)
	}

	return &Document{
		Meta: Meta{
			ToolVersion: toolVersion,
			Timestamp:   time.Now().UTC().Format(time.RFC3339),
			GoVersion:   runtime.Version(),
			CPUModel:    detectCPUModel(),
			CPUCores:    logicalCPUs,
			CPUThreads:  logicalCPUs,
			OS:          runtime.GOOS,
			Arch:        runtime.GOARCH,
		},
		Config: Config{
			DurationS: cfg.Duration.Seconds(),
			WarmupS:   cfg.Warmup.Seconds(),
			Size:      size,
			Profile:   score.Profile,
			Weights: map[string]float64{
				"integer":    score.Weights.Integer,
				"memory":     score.Weights.Memory,
				"throughput": score.Weights.Throughput,
				"latency":    score.Weights.Latency,
			},
		},
		Scores: Scores{
			SingleThread: toModeScore(score.SingleThread),
			MultiThread:  toModeScore(score.MultiThread),
		},
		Raw: raw,
	}
}

func Marshal(d *Document) ([]byte, error) {
	return json.MarshalIndent(d, "", "  ")
}

func toRaw(r *results.RawResult) RawBenchmark {
	return RawBenchmark{
		OpsSec:     r.OpsPerSec,
		MedianNs:   r.MedianNs,
		P95Ns:      r.P95Ns,
		P99Ns:      r.P99Ns,
		Iterations: r.Iterations,
	}
}

func toModeScore(m scoring.ModeScore) ModeScore {
	return ModeScore{
		Integer:    m.Integer,
		Memory:     m.Memory,
		Throughput: m.Throughput,
		Latency:    m.Latency,
		Total:      m.Total,
		Label:      m.Label,
	}
}

func detectCPUModel() string {
	if runtime.GOOS == "linux" {
		// x86 has "model name" in /proc/cpuinfo; ARM typically does not.
		if m := readLinuxCPUModel(); m != "" {
			return m
		}
		// SBCs and embedded boards expose a human-readable board name here.
		if m := readDeviceTreeModel(); m != "" {
			if topo := summarizeARMTopology(); topo != "" {
				return m + " (" + topo + ")"
			}
			return m
		}
		if topo := summarizeARMTopology(); topo != "" {
			return topo
		}
	}
	return fmt.Sprintf("unknown (%s/%s)", runtime.GOOS, runtime.GOARCH)
}

func readLinuxCPUModel() string {
	f, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "model name") {
			if i := strings.Index(line, ":"); i >= 0 {
				return strings.TrimSpace(line[i+1:])
			}
		}
	}
	return ""
}

func readDeviceTreeModel() string {
	b, err := os.ReadFile("/proc/device-tree/model")
	if err != nil {
		return ""
	}
	// device-tree strings are NUL-terminated.
	return strings.TrimSpace(strings.Trim(string(b), "\x00"))
}

// summarizeARMTopology aggregates heterogeneous ARM cores from /proc/cpuinfo
// into a compact label like "4× Cortex-A55 + 4× Cortex-A78".
// Returns "" on non-ARM or when the cpuinfo layout is not recognized.
func summarizeARMTopology() string {
	f, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return ""
	}
	defer f.Close()

	type key struct{ impl, part string }
	counts := make(map[key]int)
	var order []key
	var cur key

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			if cur.impl != "" && cur.part != "" {
				if _, seen := counts[cur]; !seen {
					order = append(order, cur)
				}
				counts[cur]++
			}
			cur = key{}
			continue
		}
		colon := strings.Index(line, ":")
		if colon < 0 {
			continue
		}
		k := strings.TrimSpace(line[:colon])
		v := strings.TrimSpace(line[colon+1:])
		switch k {
		case "CPU implementer":
			cur.impl = v
		case "CPU part":
			cur.part = v
		}
	}
	if cur.impl != "" && cur.part != "" {
		if _, seen := counts[cur]; !seen {
			order = append(order, cur)
		}
		counts[cur]++
	}
	if len(counts) == 0 {
		return ""
	}

	parts := make([]string, 0, len(order))
	for _, k := range order {
		parts = append(parts, fmt.Sprintf("%d× %s", counts[k], armCoreName(k.impl, k.part)))
	}
	return strings.Join(parts, " + ")
}

// armCoreName maps ARM (0x41) implementer + part IDs to readable names.
// Non-exhaustive — covers the cores commonly seen in SBCs, phones, servers.
func armCoreName(impl, part string) string {
	if impl != "0x41" {
		return fmt.Sprintf("%s:%s", impl, part)
	}
	names := map[string]string{
		"0xd03": "Cortex-A53",
		"0xd04": "Cortex-A35",
		"0xd05": "Cortex-A55",
		"0xd07": "Cortex-A57",
		"0xd08": "Cortex-A72",
		"0xd09": "Cortex-A73",
		"0xd0a": "Cortex-A75",
		"0xd0b": "Cortex-A76",
		"0xd0c": "Neoverse-N1",
		"0xd0d": "Cortex-A77",
		"0xd40": "Neoverse-V1",
		"0xd41": "Cortex-A78",
		"0xd44": "Cortex-X1",
		"0xd46": "Cortex-A510",
		"0xd47": "Cortex-A710",
		"0xd48": "Cortex-X2",
		"0xd49": "Neoverse-N2",
		"0xd4d": "Cortex-A715",
		"0xd4e": "Cortex-X3",
		"0xd4f": "Neoverse-V2",
	}
	if n, ok := names[part]; ok {
		return n
	}
	return "ARM " + part
}
