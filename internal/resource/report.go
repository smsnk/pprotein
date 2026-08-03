package resource

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// DefaultTopN は要約に出すプロセスの件数。
const DefaultTopN = 12

type (
	// Summary は Report を集計した結果。CLI やグラフからも使う。
	Summary struct {
		Host       string
		StartedAt  time.Time
		FinishedAt time.Time
		NumCPU     int
		Samples    int

		CPU  CPUSummary
		Load MinMaxAvg
		Mem  MemSummary

		Disks     []DiskSummary
		Processes []ProcessSummary
	}

	// CPUSummary はホスト全体の CPU 内訳（%）。Busy は idle 以外の合計。
	CPUSummary struct {
		User, System, Iowait, Steal, Irq MinMaxAvg
		Busy                             MinMaxAvg
	}

	MemSummary struct {
		TotalKB  uint64
		UsedKB   MinMaxAvg // MemTotal - MemAvailable
		SwapUsed MinMaxAvg
	}

	DiskSummary struct {
		Name      string
		ReadKBps  MinMaxAvg
		WriteKBps MinMaxAvg
		UtilPct   MinMaxAvg
	}

	// ProcessSummary はプロセス単位の CPU（コア1本を100%とした値）と RSS。
	ProcessSummary struct {
		Pid     int
		Command string
		CPU     MinMaxAvg
		RssKB   MinMaxAvg
	}

	MinMaxAvg struct {
		Min, Max, Avg float64

		sum float64
		n   int
	}
)

func (m *MinMaxAvg) add(v float64) {
	if m.n == 0 {
		m.Min, m.Max = v, v
	} else {
		if v < m.Min {
			m.Min = v
		}
		if v > m.Max {
			m.Max = v
		}
	}
	m.sum += v
	m.n++
}

func (m *MinMaxAvg) finish() {
	if m.n > 0 {
		m.Avg = m.sum / float64(m.n)
	}
}

// Summarize は隣り合うサンプルの差分から走行中の平均・ピークを出す。
func Summarize(r *Report, topN int) *Summary {
	if topN <= 0 {
		topN = DefaultTopN
	}

	s := &Summary{
		Host:       r.Host,
		StartedAt:  r.StartedAt,
		FinishedAt: r.FinishedAt,
		Samples:    len(r.Samples),
	}
	if len(r.Samples) > 0 {
		s.NumCPU = r.Samples[0].NumCPU
		s.Mem.TotalKB = r.Samples[0].MemTotalKB
	}
	if len(r.Samples) < 2 {
		return s // 差分が取れない
	}

	type procAcc struct {
		command string
		cpu     MinMaxAvg
		rss     MinMaxAvg
	}
	procs := map[int]*procAcc{}
	disks := map[string]*DiskSummary{}

	for i := 1; i < len(r.Samples); i++ {
		prev, cur := r.Samples[i-1], r.Samples[i]
		elapsed := cur.At.Sub(prev.At).Seconds()
		if elapsed <= 0 {
			continue
		}
		// --- ホストの CPU ---
		d := cur.CPU.Sub(prev.CPU)
		total := float64(d.Total())
		if total > 0 {
			s.CPU.User.add(float64(d.User+d.Nice) / total * 100)
			s.CPU.System.add(float64(d.System) / total * 100)
			s.CPU.Iowait.add(float64(d.Iowait) / total * 100)
			s.CPU.Steal.add(float64(d.Steal) / total * 100)
			s.CPU.Irq.add(float64(d.Irq+d.SoftIrq) / total * 100)
			s.CPU.Busy.add(float64(d.Total()-d.Idle) / total * 100)
		}

		s.Load.add(cur.Load1)
		if cur.MemTotalKB > 0 {
			s.Mem.UsedKB.add(float64(cur.MemTotalKB - cur.MemAvailableKB))
		}
		if cur.SwapTotalKB > 0 {
			s.Mem.SwapUsed.add(float64(cur.SwapTotalKB - cur.SwapFreeKB))
		}

		// --- ディスク ---
		for name, ds := range cur.Disks {
			pds, ok := prev.Disks[name]
			if !ok {
				continue
			}
			sum, ok := disks[name]
			if !ok {
				sum = &DiskSummary{Name: name}
				disks[name] = sum
			}
			sum.ReadKBps.add(float64(subu(ds.ReadSectors, pds.ReadSectors)) / 2 / elapsed)
			sum.WriteKBps.add(float64(subu(ds.WriteSectors, pds.WriteSectors)) / 2 / elapsed)
			sum.UtilPct.add(float64(subu(ds.IoMs, pds.IoMs)) / (elapsed * 1000) * 100)
		}

		// --- プロセス ---
		// CPU は「全 CPU の jiffies 差分に対する比 × コア数」で出す。
		// USER_HZ を仮定せずに済み、コア1本を 100% とした値になる。
		prevProc := map[int]Process{}
		for _, p := range prev.Processes {
			prevProc[p.Pid] = p
		}
		for _, p := range cur.Processes {
			pp, ok := prevProc[p.Pid]
			if !ok || pp.Command != p.Command {
				continue // 走行中に生まれたプロセス / pid の使い回し
			}
			acc, ok := procs[p.Pid]
			if !ok {
				acc = &procAcc{command: p.Command}
				procs[p.Pid] = acc
			}
			cpuPct := 0.0
			if total > 0 {
				cpuPct = float64(subu(p.CPU, pp.CPU)) / total * 100 * float64(cur.NumCPU)
			}
			acc.cpu.add(cpuPct)
			acc.rss.add(float64(p.RssKB))
		}
	}

	s.CPU.User.finish()
	s.CPU.System.finish()
	s.CPU.Iowait.finish()
	s.CPU.Steal.finish()
	s.CPU.Irq.finish()
	s.CPU.Busy.finish()
	s.Load.finish()
	s.Mem.UsedKB.finish()
	s.Mem.SwapUsed.finish()

	for _, d := range disks {
		d.ReadKBps.finish()
		d.WriteKBps.finish()
		d.UtilPct.finish()
		s.Disks = append(s.Disks, *d)
	}
	sort.Slice(s.Disks, func(i, j int) bool {
		return s.Disks[i].WriteKBps.Avg+s.Disks[i].ReadKBps.Avg >
			s.Disks[j].WriteKBps.Avg+s.Disks[j].ReadKBps.Avg
	})

	for pid, acc := range procs {
		acc.cpu.finish()
		acc.rss.finish()
		s.Processes = append(s.Processes, ProcessSummary{
			Pid: pid, Command: acc.command, CPU: acc.cpu, RssKB: acc.rss,
		})
	}
	sort.Slice(s.Processes, func(i, j int) bool {
		if s.Processes[i].CPU.Avg != s.Processes[j].CPU.Avg {
			return s.Processes[i].CPU.Avg > s.Processes[j].CPU.Avg
		}
		return s.Processes[i].RssKB.Avg > s.Processes[j].RssKB.Avg
	})
	if len(s.Processes) > topN {
		s.Processes = s.Processes[:topN]
	}

	return s
}

// Text は要約を人間とエージェントが読めるテキストにする。
func (s *Summary) Text() string {
	var b strings.Builder

	fmt.Fprintf(&b, "host: %s (%d cpu)\n", s.Host, s.NumCPU)
	fmt.Fprintf(&b, "window: %s - %s (%d samples)\n\n",
		s.StartedAt.Format("15:04:05"), s.FinishedAt.Format("15:04:05"), s.Samples)

	if s.Samples < 2 {
		b.WriteString("サンプルが足りません（Duration を interval より長くしてください）\n")
		return b.String()
	}

	b.WriteString("[host cpu %]\n")
	fmt.Fprintf(&b, "%-8s %8s %8s\n", "", "avg", "peak")
	writeRow(&b, "busy", s.CPU.Busy)
	writeRow(&b, "user", s.CPU.User)
	writeRow(&b, "system", s.CPU.System)
	writeRow(&b, "iowait", s.CPU.Iowait)
	writeRow(&b, "steal", s.CPU.Steal)
	writeRow(&b, "irq", s.CPU.Irq)

	fmt.Fprintf(&b, "\n[load1] avg=%.2f peak=%.2f\n", s.Load.Avg, s.Load.Max)

	if s.Mem.TotalKB > 0 {
		fmt.Fprintf(&b, "[mem] total=%s used avg=%s peak=%s (%.1f%%)\n",
			humanKB(float64(s.Mem.TotalKB)), humanKB(s.Mem.UsedKB.Avg), humanKB(s.Mem.UsedKB.Max),
			s.Mem.UsedKB.Max/float64(s.Mem.TotalKB)*100)
	}
	if s.Mem.SwapUsed.Max > 0 {
		fmt.Fprintf(&b, "[swap] used avg=%s peak=%s\n",
			humanKB(s.Mem.SwapUsed.Avg), humanKB(s.Mem.SwapUsed.Max))
	}

	if len(s.Disks) > 0 {
		b.WriteString("\n[disk]\n")
		fmt.Fprintf(&b, "%-10s %12s %12s %8s\n", "device", "read KB/s", "write KB/s", "util%")
		for _, d := range s.Disks {
			fmt.Fprintf(&b, "%-10s %12.1f %12.1f %8.1f\n",
				d.Name, d.ReadKBps.Avg, d.WriteKBps.Avg, d.UtilPct.Avg)
		}
	}

	b.WriteString("\n[top processes] cpu% はコア1本を100%とした値\n")
	fmt.Fprintf(&b, "%8s %8s %10s %10s  %s\n", "cpu% avg", "peak", "rss avg", "rss peak", "command")
	for _, p := range s.Processes {
		fmt.Fprintf(&b, "%8.1f %8.1f %10s %10s  %s (%d)\n",
			p.CPU.Avg, p.CPU.Max, humanKB(p.RssKB.Avg), humanKB(p.RssKB.Max), p.Command, p.Pid)
	}

	return b.String()
}

func writeRow(b *strings.Builder, name string, v MinMaxAvg) {
	fmt.Fprintf(b, "%-8s %8.1f %8.1f\n", name, v.Avg, v.Max)
}

func humanKB(kb float64) string {
	switch {
	case kb >= 1024*1024:
		return fmt.Sprintf("%.1fG", kb/1024/1024)
	case kb >= 1024:
		return fmt.Sprintf("%.1fM", kb/1024)
	default:
		return fmt.Sprintf("%.0fK", kb)
	}
}
