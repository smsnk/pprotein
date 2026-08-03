// Package resource は走行中のホスト/プロセスのリソース使用状況を計測する。
//
// alp / slow query log / pprof は「アプリと DB の中で何が遅いか」は教えてくれるが、
// どのプロセスがどのホストの CPU / メモリを食い切っているかは分からない。
// 走行中に ssh して top を眺めるのをやめ、他の計測と同じ収集として残せるようにする。
//
// 取得は /proc の直読み。外部コマンドに依存しない。
package resource

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ProcRoot は /proc の位置。テストで差し替えられるようにしてある。
var ProcRoot = "/proc"

type (
	// Report は1回の走行分の計測結果。snapshot の本体としてこの JSON が保存される。
	Report struct {
		Host       string
		StartedAt  time.Time
		FinishedAt time.Time
		IntervalMs int64
		Samples    []Sample
	}

	// Sample は1時点のスナップショット。CPU とディスクは累積値なので、
	// 集計時に隣り合うサンプルの差分を取る。
	Sample struct {
		At             time.Time
		CPU            CPUTimes
		NumCPU         int
		Load1          float64
		MemTotalKB     uint64
		MemAvailableKB uint64
		SwapTotalKB    uint64
		SwapFreeKB     uint64
		Disks          map[string]DiskStat
		Processes      []Process
	}

	// CPUTimes は /proc/stat の cpu 行（累積 jiffies）。
	CPUTimes struct {
		User, Nice, System, Idle, Iowait, Irq, SoftIrq, Steal uint64
	}

	// DiskStat は /proc/diskstats（累積）。Sectors は 512B 単位。
	DiskStat struct {
		ReadSectors  uint64
		WriteSectors uint64
		IoMs         uint64
	}

	// Process は1プロセスの累積 CPU jiffies と RSS。
	Process struct {
		Pid     int
		Command string
		CPU     uint64 // utime + stime（累積 jiffies）
		RssKB   uint64
	}
)

func (c CPUTimes) Total() uint64 {
	return c.User + c.Nice + c.System + c.Idle + c.Iowait + c.Irq + c.SoftIrq + c.Steal
}

func (c CPUTimes) Sub(o CPUTimes) CPUTimes {
	return CPUTimes{
		User: subu(c.User, o.User), Nice: subu(c.Nice, o.Nice),
		System: subu(c.System, o.System), Idle: subu(c.Idle, o.Idle),
		Iowait: subu(c.Iowait, o.Iowait), Irq: subu(c.Irq, o.Irq),
		SoftIrq: subu(c.SoftIrq, o.SoftIrq), Steal: subu(c.Steal, o.Steal),
	}
}

// subu はカウンタが巻き戻ったとき（プロセス再起動など）に 0 を返す。
func subu(a, b uint64) uint64 {
	if a < b {
		return 0
	}
	return a - b
}

// Collect は duration の間 interval おきにサンプリングする。
// 最低2サンプル取らないと差分が計算できないので、必ず両端を含める。
func Collect(duration, interval time.Duration) (*Report, error) {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	if duration < interval {
		duration = interval
	}

	host, _ := os.Hostname()
	report := &Report{
		Host:       host,
		StartedAt:  time.Now(),
		IntervalMs: interval.Milliseconds(),
	}

	deadline := time.Now().Add(duration)
	for {
		s, err := Sampling()
		if err != nil {
			return nil, err
		}
		report.Samples = append(report.Samples, *s)

		if !time.Now().Before(deadline) {
			break
		}
		if remain := time.Until(deadline); remain < interval {
			time.Sleep(remain)
		} else {
			time.Sleep(interval)
		}
	}

	report.FinishedAt = time.Now()
	return report, nil
}

// Sampling は1時点のリソース使用状況を読む。
func Sampling() (*Sample, error) {
	s := &Sample{At: time.Now()}

	cpu, n, err := readStat()
	if err != nil {
		return nil, err
	}
	s.CPU, s.NumCPU = cpu, n

	s.Load1 = readLoadAvg()
	s.MemTotalKB, s.MemAvailableKB, s.SwapTotalKB, s.SwapFreeKB = readMeminfo()
	s.Disks = readDiskstats()

	procs, err := readProcesses()
	if err != nil {
		return nil, err
	}
	s.Processes = procs

	return s, nil
}

func readStat() (CPUTimes, int, error) {
	f, err := os.Open(filepath.Join(ProcRoot, "stat"))
	if err != nil {
		return CPUTimes{}, 0, fmt.Errorf("failed to read %s/stat: %w", ProcRoot, err)
	}
	defer f.Close()

	var cpu CPUTimes
	ncpu := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 || !strings.HasPrefix(fields[0], "cpu") {
			continue
		}
		if fields[0] != "cpu" {
			ncpu++
			continue
		}
		v := make([]uint64, 8)
		for i := 0; i < 8 && i+1 < len(fields); i++ {
			v[i], _ = strconv.ParseUint(fields[i+1], 10, 64)
		}
		cpu = CPUTimes{v[0], v[1], v[2], v[3], v[4], v[5], v[6], v[7]}
	}
	if ncpu == 0 {
		ncpu = 1
	}
	return cpu, ncpu, sc.Err()
}

func readLoadAvg() float64 {
	buf, err := os.ReadFile(filepath.Join(ProcRoot, "loadavg"))
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(buf))
	if len(fields) == 0 {
		return 0
	}
	v, _ := strconv.ParseFloat(fields[0], 64)
	return v
}

func readMeminfo() (total, available, swapTotal, swapFree uint64) {
	f, err := os.Open(filepath.Join(ProcRoot, "meminfo"))
	if err != nil {
		return
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		v, _ := strconv.ParseUint(fields[1], 10, 64)
		switch fields[0] {
		case "MemTotal:":
			total = v
		case "MemAvailable:":
			available = v
		case "SwapTotal:":
			swapTotal = v
		case "SwapFree:":
			swapFree = v
		}
	}
	return
}

func readDiskstats() map[string]DiskStat {
	f, err := os.Open(filepath.Join(ProcRoot, "diskstats"))
	if err != nil {
		return nil
	}
	defer f.Close()

	stats := map[string]DiskStat{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 13 {
			continue
		}
		name := fields[2]
		// ループバックや ramdisk は見ても仕方がない
		if strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "ram") {
			continue
		}
		readSectors, _ := strconv.ParseUint(fields[5], 10, 64)
		writeSectors, _ := strconv.ParseUint(fields[9], 10, 64)
		ioMs, _ := strconv.ParseUint(fields[12], 10, 64)
		if readSectors == 0 && writeSectors == 0 {
			continue
		}
		stats[name] = DiskStat{ReadSectors: readSectors, WriteSectors: writeSectors, IoMs: ioMs}
	}
	return stats
}

func readProcesses() ([]Process, error) {
	entries, err := os.ReadDir(ProcRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", ProcRoot, err)
	}

	procs := make([]Process, 0, 128)
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		p, ok := readProcess(pid)
		if ok {
			procs = append(procs, p)
		}
	}
	return procs, nil
}

func readProcess(pid int) (Process, bool) {
	buf, err := os.ReadFile(filepath.Join(ProcRoot, strconv.Itoa(pid), "stat"))
	if err != nil {
		return Process{}, false // 読んでいる間に終了したプロセス
	}
	line := string(buf)

	// comm は括弧で囲まれ、空白や括弧を含みうる。最後の ')' より後ろを見る。
	end := strings.LastIndex(line, ")")
	start := strings.Index(line, "(")
	if start < 0 || end < 0 || end < start {
		return Process{}, false
	}
	command := line[start+1 : end]

	fields := strings.Fields(line[end+1:])
	// fields[0] は state。以降は /proc/[pid]/stat の 3 番目のフィールドから。
	// utime = 14番目, stime = 15番目, rss = 24番目（1-indexed）
	if len(fields) < 22 {
		return Process{}, false
	}
	utime, _ := strconv.ParseUint(fields[11], 10, 64)
	stime, _ := strconv.ParseUint(fields[12], 10, 64)
	rssPages, _ := strconv.ParseUint(fields[21], 10, 64)

	return Process{
		Pid:     pid,
		Command: command,
		CPU:     utime + stime,
		RssKB:   rssPages * uint64(os.Getpagesize()) / 1024,
	}, true
}
