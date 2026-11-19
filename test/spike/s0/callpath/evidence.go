// SPDX-License-Identifier: Apache-2.0

package callpath

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"time"
)

// repoRoot is relative to the package directory, where go test executes.
const repoRoot = "../../../.."

// Production Go of every package linked into the measured executable, plus the
// whole callpath package (its test files are compiled into the test binary).
var closureClasses = []string{
	"sdk/kernel", "test/spike/s0/concurrency", "test/spike/s0/composition", "test/spike/s0/contextmodel",
	"test/spike/s0/internal/eventqueue", "test/spike/s0/scale",
}

const callpathClass = "test/spike/s0/callpath"

var closureExtras = []string{"go.mod", "go.sum", "docs/design/s0-spike-criteria.md", "docs/design/s0-criteria-freeze.json", "docs/design/s0-callpath-protocol-amendment-1.md", "docs/design/s0-callpath-protocol-amendment-2.md"}

type ClosureFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Bytes  int    `json:"bytes"`
}

type Closure struct {
	SHA256  string        `json:"sha256"`
	Classes []string      `json:"sourceClasses"`
	Files   []ClosureFile `json:"files"`
}

// sourceClosure hashes the actual linked Go inputs (not unrelated SQL or draft
// output) in sorted path order, as the scale harness does.
func sourceClosure(root string) (Closure, error) {
	paths := slices.Clone(closureExtras)
	add := func(class string, tests bool) error {
		matches, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(class), "*.go"))
		if err != nil || len(matches) == 0 {
			return fmt.Errorf("source class %s: matches=%d: %v", class, len(matches), err)
		}
		for _, m := range matches {
			if !tests && strings.HasSuffix(m, "_test.go") {
				continue
			}
			rel, err := filepath.Rel(root, m)
			if err != nil {
				return err
			}
			paths = append(paths, filepath.ToSlash(rel))
		}
		return nil
	}
	for _, class := range closureClasses {
		if err := add(class, false); err != nil {
			return Closure{}, err
		}
	}
	if err := add(callpathClass, true); err != nil {
		return Closure{}, err
	}
	paths = append(paths, callpathClass+"/README.md")
	slices.Sort(paths)
	c := Closure{Classes: append(slices.Clone(closureClasses), callpathClass)}
	h := sha256.New()
	for _, p := range paths {
		text, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(p)))
		if err != nil {
			return c, err
		}
		digest := fmt.Sprintf("%x", sha256.Sum256(text))
		c.Files = append(c.Files, ClosureFile{Path: p, SHA256: digest, Bytes: len(text)})
		fmt.Fprintf(h, "%s\x00%s\n", p, digest)
	}
	c.SHA256 = hex.EncodeToString(h.Sum(nil))
	return c, nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	_, err = io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil)), errors.Join(err, f.Close())
}

// Identity is what only the parent knows. It is supplied as strict JSON. The
// runner computes the three digests itself; Expected* digests, when supplied,
// must match what the runner computed.
type Identity struct {
	BaselineCommit           string   `json:"baselineCommit"`
	Commands                 []string `json:"commands"`
	FinalParity              bool     `json:"finalParityCompleted"`
	HostEvidence             string   `json:"hostEvidence"`
	ExpectedCriteriaSHA256   string   `json:"expectedCriteriaSHA256,omitempty"`
	ExpectedSourceSHA256     string   `json:"expectedSourceSHA256,omitempty"`
	ExpectedExecutableSHA256 string   `json:"expectedExecutableSHA256,omitempty"`
}

// LoadIdentity reads a strict identity JSON file.
func LoadIdentity(path string) (Identity, error) {
	var id Identity
	f, err := os.Open(path)
	if err != nil {
		return id, err
	}
	d := json.NewDecoder(f)
	d.DisallowUnknownFields()
	return id, errors.Join(d.Decode(&id), f.Close())
}

// IdentityRecord is the invocation/identity record written before any run.
type IdentityRecord struct {
	Schema         string    `json:"schema"`
	Protocol       int       `json:"protocolAmendment"`
	Mode           string    `json:"mode"`
	Acceptance     bool      `json:"acceptanceEligible"`
	StartedUTC     time.Time `json:"startedUTC"`
	Supplied       *Identity `json:"parentSupplied"`
	CriteriaSHA256 string    `json:"criteriaSHA256"`
	FreezeSHA256   string    `json:"freezeReceiptCriteriaSHA256"`
	SourceSHA256   string    `json:"sourceClosureSHA256"`
	Executable     string    `json:"executableSHA256"`
	// Args omits the executable's directory; paths are never recorded.
	Args       []string          `json:"arguments"`
	GoVersion  string            `json:"goVersion"`
	GOOS       string            `json:"goos"`
	GOARCH     string            `json:"goarch"`
	GOMAXPROCS int               `json:"gomaxprocs"`
	NumCPU     int               `json:"visibleLogicalCPUs"`
	Affinity   string            `json:"cpuAffinity"`
	CPUModel   string            `json:"cpuModel"`
	Kernel     string            `json:"kernel"`
	CPUMax     string            `json:"cgroupCPUMax"`
	Env        map[string]string `json:"environment"`
	Race       bool              `json:"raceDetector"`
	Build      map[string]string `json:"buildSettings"`
	Plan       Plan              `json:"plan"`
	// Problems are missing/mismatched identity facts; each makes the run inconclusive.
	Problems []string `json:"identityProblems"`
	// Invalid are protocol-environment violations; each makes the run invalid.
	Invalid []string `json:"protocolViolations"`
}

func readTrim(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func procField(file, field string) string {
	for _, line := range strings.Split(readTrim(file), "\n") {
		if k, v, ok := strings.Cut(line, ":"); ok && strings.TrimSpace(k) == field {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func validDigest(v string) bool { b, err := hex.DecodeString(v); return err == nil && len(b) == 32 }

// CollectIdentity gathers everything the process can observe itself and checks
// the parent-supplied facts. It never invents a missing fact.
func CollectIdentity(root string, plan Plan, dry bool, supplied *Identity) (IdentityRecord, Closure) {
	rec := IdentityRecord{Schema: "s0-callpath-identity/v1", Protocol: ProtocolAmendment, StartedUTC: time.Now().UTC(), Supplied: supplied, GoVersion: runtime.Version(),
		GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, GOMAXPROCS: runtime.GOMAXPROCS(0), NumCPU: runtime.NumCPU(), Race: raceEnabled, Plan: plan,
		Mode: "full protocol; candidate evidence, never final-port evidence", Acceptance: !dry && plan.Frozen(), Build: map[string]string{}, Env: map[string]string{}}
	if dry {
		rec.Mode = "DRY RUN - NOT ACCEPTANCE - never satisfies the S0 gate"
	}
	problem := func(format string, args ...any) { rec.Problems = append(rec.Problems, fmt.Sprintf(format, args...)) }
	for _, name := range []string{"GOFLAGS", "GOGC", "GOMEMLIMIT", "GODEBUG", "GOMAXPROCS", "GOAMD64"} {
		rec.Env[name] = os.Getenv(name)
	}
	if len(os.Args) > 0 {
		rec.Args = []string{filepath.Base(os.Args[0])}
		for _, a := range os.Args[1:] {
			if k, v, ok := strings.Cut(a, "="); ok && strings.Contains(v, "/") {
				a = k + "=<path>" // Paths (temp dirs, output dirs) are never recorded.
			}
			rec.Args = append(rec.Args, a)
		}
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		rec.Build["path"], rec.Build["main"] = bi.Path, bi.Main.Path
		for _, s := range bi.Settings {
			rec.Build[s.Key] = s.Value
		}
	}
	rec.Affinity = procField("/proc/self/status", "Cpus_allowed_list")
	rec.CPUModel = procField("/proc/cpuinfo", "model name")
	rec.Kernel = readTrim("/proc/sys/kernel/osrelease")
	rec.CPUMax = readTrim("/sys/fs/cgroup/cpu.max")
	if rec.Affinity == "" {
		problem("CPU affinity unavailable")
	}
	if rec.Kernel == "" {
		problem("kernel release unavailable")
	}

	closure, err := sourceClosure(root)
	if err != nil {
		problem("source closure: %v", err)
	}
	rec.SourceSHA256 = closure.SHA256
	if exe, err := os.Executable(); err != nil {
		problem("executable path: %v", err)
	} else if rec.Executable, err = fileSHA256(exe); err != nil {
		problem("executable hash: %v", err)
	}
	if rec.CriteriaSHA256, err = fileSHA256(filepath.Join(root, "docs/design/s0-spike-criteria.md")); err != nil {
		problem("criteria hash: %v", err)
	}
	var freeze struct {
		Criteria struct {
			SHA256 string `json:"sha256"`
		} `json:"criteria"`
	}
	if b, err := os.ReadFile(filepath.Join(root, "docs/design/s0-criteria-freeze.json")); err != nil {
		problem("freeze receipt: %v", err)
	} else if err := json.Unmarshal(b, &freeze); err != nil || !validDigest(freeze.Criteria.SHA256) {
		problem("freeze receipt has no criteria digest")
	} else {
		rec.FreezeSHA256 = freeze.Criteria.SHA256
		if rec.CriteriaSHA256 != "" && rec.CriteriaSHA256 != rec.FreezeSHA256 {
			rec.Invalid = append(rec.Invalid, "criteria bytes differ from the freeze receipt digest")
		}
	}

	if supplied == nil {
		problem("parent identity JSON missing (baseline commit, exact commands, final parity, host evidence)")
	} else {
		if supplied.BaselineCommit == "" {
			problem("baselineCommit missing")
		}
		if len(supplied.Commands) == 0 {
			problem("exact commands missing")
		}
		if !supplied.FinalParity {
			problem("finalParityCompleted is not true")
		}
		if supplied.HostEvidence == "" {
			problem("hostEvidence missing")
		}
		for _, c := range [][2]string{{supplied.ExpectedCriteriaSHA256, rec.CriteriaSHA256}, {supplied.ExpectedSourceSHA256, rec.SourceSHA256}, {supplied.ExpectedExecutableSHA256, rec.Executable}} {
			if c[0] != "" && c[0] != c[1] {
				rec.Invalid = append(rec.Invalid, "expected digest "+c[0]+" differs from computed "+c[1])
			}
		}
	}
	if rec.GOMAXPROCS != RequiredGOMAXPROCS {
		rec.Invalid = append(rec.Invalid, fmt.Sprintf("GOMAXPROCS=%d, protocol requires %d", rec.GOMAXPROCS, RequiredGOMAXPROCS))
	}
	if raceEnabled {
		rec.Invalid = append(rec.Invalid, "race-instrumented build; optimized performance is measured without the race detector")
	}
	return rec, closure
}

// HostSample is one resource/noise observation. Unobservable counters stay
// unobserved: a missing file is never read as zero.
type HostSample struct {
	UTC             time.Time `json:"utc"`
	SwapIn          uint64    `json:"swapIn"`
	SwapOut         uint64    `json:"swapOut"`
	SwapObserved    bool      `json:"swapObserved"`
	Throttled       uint64    `json:"cgroupThrottledPeriods"`
	ThrottleSeen    bool      `json:"throttleObserved"`
	Load1           string    `json:"load1"`
	MemAvailableKiB uint64    `json:"memAvailableKiB"`
	SwapFreeKiB     uint64    `json:"swapFreeKiB"`
	Goroutines      int       `json:"goroutines"`
}

func kv(text, key string) (uint64, bool) {
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && strings.TrimSuffix(f[0], ":") == key {
			n, err := strconv.ParseUint(f[1], 10, 64)
			return n, err == nil
		}
	}
	return 0, false
}

func ReadHost() HostSample {
	s := HostSample{UTC: time.Now().UTC(), Goroutines: runtime.NumGoroutine()}
	vm := readTrim("/proc/vmstat")
	in, okIn := kv(vm, "pswpin")
	out, okOut := kv(vm, "pswpout")
	s.SwapIn, s.SwapOut, s.SwapObserved = in, out, okIn && okOut
	paths := []string{"/sys/fs/cgroup/cpu.stat"}
	for _, line := range strings.Split(readTrim("/proc/self/cgroup"), "\n") {
		if rest, ok := strings.CutPrefix(line, "0::"); ok && rest != "/" {
			paths = append([]string{"/sys/fs/cgroup" + rest + "/cpu.stat"}, paths...)
		}
	}
	for _, p := range paths {
		if n, ok := kv(readTrim(p), "nr_throttled"); ok {
			s.Throttled, s.ThrottleSeen = n, true
			break
		}
	}
	if f := strings.Fields(readTrim("/proc/loadavg")); len(f) > 0 {
		s.Load1 = f[0]
	}
	mi := readTrim("/proc/meminfo")
	s.MemAvailableKiB, _ = kv(mi, "MemAvailable")
	s.SwapFreeKiB, _ = kv(mi, "SwapFree")
	return s
}

// HostDelta is the evaluation view of one run's before/after samples.
type HostDelta struct {
	Run          string `json:"run"`
	SwapDelta    uint64 `json:"swapPagesMoved"`
	ThrottleDiff uint64 `json:"throttledPeriods"`
	SwapKnown    bool   `json:"swapObserved"`
	ThrottleKnow bool   `json:"throttleObserved"`
}

func hostDelta(run string, before, after HostSample) HostDelta {
	return HostDelta{Run: run, SwapKnown: before.SwapObserved && after.SwapObserved, ThrottleKnow: before.ThrottleSeen && after.ThrottleSeen,
		SwapDelta:    (after.SwapIn - before.SwapIn) + (after.SwapOut - before.SwapOut),
		ThrottleDiff: after.Throttled - before.Throttled}
}

// ManifestEntry binds a raw file to the bytes written.
type ManifestEntry struct {
	Name   string `json:"name"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

// evidenceWriter creates every file exclusively; nothing is overwritten.
type evidenceWriter struct {
	dir      string
	manifest []ManifestEntry
}

func (w *evidenceWriter) write(name string, value any) error {
	f, err := os.OpenFile(filepath.Join(w.dir, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	h := sha256.New()
	buf := bufio.NewWriterSize(io.MultiWriter(f, h), 1<<20)
	counter := &countingWriter{w: buf}
	err = json.NewEncoder(counter).Encode(value)
	err = errors.Join(err, buf.Flush(), f.Close())
	if err != nil {
		return err
	}
	w.manifest = append(w.manifest, ManifestEntry{Name: name, Bytes: counter.n, SHA256: hex.EncodeToString(h.Sum(nil))})
	return nil
}

type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// verify re-reads every recorded file and returns each discrepancy.
func (w *evidenceWriter) verify() []string {
	var problems []string
	for _, e := range w.manifest {
		sum, err := fileSHA256(filepath.Join(w.dir, e.Name))
		switch {
		case err != nil:
			problems = append(problems, fmt.Sprintf("raw record %s unreadable: %v", e.Name, err))
		case sum != e.SHA256:
			problems = append(problems, fmt.Sprintf("raw record %s changed after writing", e.Name))
		case e.Bytes == 0:
			problems = append(problems, fmt.Sprintf("raw record %s is empty", e.Name))
		}
	}
	return problems
}
