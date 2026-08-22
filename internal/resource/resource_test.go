package resource

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeProcFixture は /proc の代わりになるディレクトリを作る。
// pids は pid -> (comm, utime, stime, rssPages)。
func writeProcFixture(t *testing.T, stat string, pids map[int][4]any) string {
	t.Helper()
	dir := t.TempDir()

	write := func(name, content string) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write("stat", stat)
	write("loadavg", "3.21 2.10 1.05 2/512 12345\n")
	write("meminfo", strings.Join([]string{
		"MemTotal:        8000000 kB",
		"MemFree:         1000000 kB",
		"MemAvailable:    2000000 kB",
		"SwapTotal:       1000000 kB",
		"SwapFree:         900000 kB",
	}, "\n")+"\n")
	// major minor name reads rdMerged rdSectors msReading writes wrMerged wrSectors msWriting inFlight ioMs ...
	write("diskstats", strings.Join([]string{
		" 259  0 nvme0n1 100 0 2048 10 200 0 4096 20 0 500 30",
		"   7  0 loop0 1 0 8 1 0 0 0 0 0 1 1",
		" 259  9 emptydev 0 0 0 0 0 0 0 0 0 0 0",
	}, "\n")+"\n")

	for pid, v := range pids {
		comm, utime, stime, rss := v[0].(string), v[1].(int), v[2].(int), v[3].(int)
		// 1:pid 2:comm 3:state 以降。utime は 14番目、stime は 15番目、rss は 24番目。
		fields := make([]string, 0, 24)
		fields = append(fields, "S") // 3:state
		for i := 4; i <= 24; i++ {
			switch i {
			case 14:
				fields = append(fields, fmt.Sprint(utime))
			case 15:
				fields = append(fields, fmt.Sprint(stime))
			case 24:
				fields = append(fields, fmt.Sprint(rss))
			default:
				fields = append(fields, "0")
			}
		}
		write(filepath.Join(fmt.Sprint(pid), "stat"),
			fmt.Sprintf("%d (%s) %s\n", pid, comm, strings.Join(fields, " ")))
	}
	return dir
}

const statLine = `cpu  1000 10 500 8000 100 0 20 30 0 0
cpu0 500 5 250 4000 50 0 10 15 0 0
cpu1 500 5 250 4000 50 0 10 15 0 0
intr 12345
ctxt 6789
`

func TestSampling(t *testing.T) {
	dir := writeProcFixture(t, statLine, map[int][4]any{
		1:    {"systemd", 10, 5, 100},
		4242: {"isuride (go)", 3000, 1000, 250000},
	})
	old := ProcRoot
	ProcRoot = dir
	defer func() { ProcRoot = old }()

	s, err := Sampling()
	if err != nil {
		t.Fatalf("Sampling: %v", err)
	}

	if s.NumCPU != 2 {
		t.Errorf("NumCPU = %d, want 2", s.NumCPU)
	}
	if s.CPU.User != 1000 || s.CPU.Iowait != 100 || s.CPU.Steal != 30 {
		t.Errorf("CPU = %+v", s.CPU)
	}
	if got, want := s.CPU.Total(), uint64(1000+10+500+8000+100+0+20+30); got != want {
		t.Errorf("CPU.Total() = %d, want %d", got, want)
	}
	if s.Load1 != 3.21 {
		t.Errorf("Load1 = %v, want 3.21", s.Load1)
	}
	if s.MemTotalKB != 8000000 || s.MemAvailableKB != 2000000 {
		t.Errorf("mem = %d/%d", s.MemTotalKB, s.MemAvailableKB)
	}
	if s.SwapTotalKB != 1000000 || s.SwapFreeKB != 900000 {
		t.Errorf("swap = %d/%d", s.SwapTotalKB, s.SwapFreeKB)
	}

	if len(s.Disks) != 1 {
		t.Errorf("Disks = %+v, want only nvme0n1 (loop と無 I/O は除外)", s.Disks)
	}
	// ioMs は13番目のフィールド（1-indexed）
	if d := s.Disks["nvme0n1"]; d.ReadSectors != 2048 || d.WriteSectors != 4096 || d.IoMs != 500 {
		t.Errorf("nvme0n1 = %+v", d)
	}

	if len(s.Processes) != 2 {
		t.Fatalf("Processes = %+v", s.Processes)
	}
	var app *Process
	for i := range s.Processes {
		if s.Processes[i].Pid == 4242 {
			app = &s.Processes[i]
		}
	}
	if app == nil {
		t.Fatal("pid 4242 が読めていない")
	}
	// comm に空白と括弧が含まれていても壊れないこと
	if app.Command != "isuride (go)" {
		t.Errorf("Command = %q, want %q", app.Command, "isuride (go)")
	}
	if app.CPU != 4000 {
		t.Errorf("CPU = %d, want 4000 (utime+stime)", app.CPU)
	}
	if want := uint64(250000 * os.Getpagesize() / 1024); app.RssKB != want {
		t.Errorf("RssKB = %d, want %d", app.RssKB, want)
	}
}

func TestSummarize(t *testing.T) {
	base := time.Date(2025, 11, 23, 10, 41, 2, 0, time.UTC)
	// 10秒で 1000 jiffies 進み、うち idle 400。busy は 60%。
	// app は 500 jiffies 使ったので、2コアなら 500/1000*100*2 = 100%（コア1本ぶん）。
	r := &Report{
		Host:      "isu1",
		StartedAt: base,
		Samples: []Sample{
			{
				At: base, NumCPU: 2, Load1: 1.0,
				CPU:            CPUTimes{User: 1000, System: 200, Idle: 5000, Iowait: 50, Steal: 10},
				MemTotalKB:     8000000,
				MemAvailableKB: 6000000,
				Disks:          map[string]DiskStat{"nvme0n1": {ReadSectors: 0, WriteSectors: 0, IoMs: 0}},
				Processes: []Process{
					{Pid: 1, Command: "mysqld", CPU: 100, RssKB: 1000},
					{Pid: 2, Command: "isuride", CPU: 200, RssKB: 2000},
				},
			},
			{
				At: base.Add(10 * time.Second), NumCPU: 2, Load1: 3.0,
				CPU:            CPUTimes{User: 1400, System: 350, Idle: 5400, Iowait: 90, Steal: 20},
				MemTotalKB:     8000000,
				MemAvailableKB: 4000000,
				Disks:          map[string]DiskStat{"nvme0n1": {ReadSectors: 2048, WriteSectors: 4096, IoMs: 3000}},
				Processes: []Process{
					{Pid: 1, Command: "mysqld", CPU: 300, RssKB: 1500},
					{Pid: 2, Command: "isuride", CPU: 700, RssKB: 4000},
				},
			},
		},
		FinishedAt: base.Add(10 * time.Second),
	}

	s := Summarize(r, 0)

	if s.NumCPU != 2 || s.Samples != 2 {
		t.Errorf("NumCPU=%d Samples=%d", s.NumCPU, s.Samples)
	}
	// 差分: user 400, system 150, idle 400, iowait 40, steal 10 -> total 1000
	if !approx(s.CPU.Busy.Avg, 60) {
		t.Errorf("Busy.Avg = %v, want 60", s.CPU.Busy.Avg)
	}
	if !approx(s.CPU.User.Avg, 40) || !approx(s.CPU.System.Avg, 15) {
		t.Errorf("user=%v system=%v", s.CPU.User.Avg, s.CPU.System.Avg)
	}
	if !approx(s.CPU.Iowait.Avg, 4) || !approx(s.CPU.Steal.Avg, 1) {
		t.Errorf("iowait=%v steal=%v", s.CPU.Iowait.Avg, s.CPU.Steal.Avg)
	}
	if !approx(s.Load.Max, 3.0) {
		t.Errorf("Load.Max = %v", s.Load.Max)
	}
	if !approx(s.Mem.UsedKB.Max, 4000000) {
		t.Errorf("Mem.UsedKB.Max = %v", s.Mem.UsedKB.Max)
	}

	if len(s.Disks) != 1 {
		t.Fatalf("Disks = %+v", s.Disks)
	}
	// 2048 sector = 1024KB を 10秒 -> 102.4 KB/s
	if !approx(s.Disks[0].ReadKBps.Avg, 102.4) || !approx(s.Disks[0].WriteKBps.Avg, 204.8) {
		t.Errorf("disk = %+v", s.Disks[0])
	}
	// 3000ms / 10000ms = 30%
	if !approx(s.Disks[0].UtilPct.Avg, 30) {
		t.Errorf("util = %v", s.Disks[0].UtilPct.Avg)
	}

	if len(s.Processes) != 2 {
		t.Fatalf("Processes = %+v", s.Processes)
	}
	// CPU の多い順に並ぶ
	if s.Processes[0].Command != "isuride" {
		t.Errorf("先頭は isuride のはず: %+v", s.Processes)
	}
	// isuride: 500/1000 * 100 * 2 = 100
	if !approx(s.Processes[0].CPU.Avg, 100) {
		t.Errorf("isuride cpu = %v, want 100", s.Processes[0].CPU.Avg)
	}
	// mysqld: 200/1000 * 100 * 2 = 40
	if !approx(s.Processes[1].CPU.Avg, 40) {
		t.Errorf("mysqld cpu = %v, want 40", s.Processes[1].CPU.Avg)
	}

	text := s.Text()
	for _, want := range []string{"isu1", "[host cpu %]", "busy", "[disk]", "nvme0n1", "isuride"} {
		if !strings.Contains(text, want) {
			t.Errorf("Text() に %q が無い:\n%s", want, text)
		}
	}
}

// pid が使い回されたときに、前のプロセスの CPU を引き継がないこと
func TestSummarizeIgnoresRecycledPid(t *testing.T) {
	base := time.Date(2025, 11, 23, 10, 0, 0, 0, time.UTC)
	r := &Report{
		Samples: []Sample{
			{At: base, NumCPU: 1, CPU: CPUTimes{Idle: 1000},
				Processes: []Process{{Pid: 7, Command: "old", CPU: 9999}}},
			{At: base.Add(time.Second), NumCPU: 1, CPU: CPUTimes{Idle: 2000},
				Processes: []Process{{Pid: 7, Command: "new", CPU: 5}}},
		},
	}
	s := Summarize(r, 0)
	if len(s.Processes) != 0 {
		t.Errorf("コマンド名が変わった pid は集計しない: %+v", s.Processes)
	}
}

func TestSummarizeWithSingleSample(t *testing.T) {
	s := Summarize(&Report{Samples: []Sample{{NumCPU: 4, MemTotalKB: 100}}}, 0)
	if s.Samples != 1 || len(s.Processes) != 0 {
		t.Errorf("サンプル1件では差分を出さない: %+v", s)
	}
	if !strings.Contains(s.Text(), "サンプルが足りません") {
		t.Errorf("Text() = %q", s.Text())
	}
}

func approx(got, want float64) bool {
	d := got - want
	return d < 0.01 && d > -0.01
}
