package sshclient

import (
	"math"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseMetricsOutputLinuxAndGPU(t *testing.T) {
	sample1 := `---CPU---
cpu  1000 0 500 8500 0 0 0 0
---MEM---
MemTotal: 8388608
MemAvailable: 2097152
---DISK---
42% /
88% /data
---NET---
1048576 524288
---LOAD---
1.25 0.90 0.80 1/200 1234
4
---GPU---
65, NVIDIA GeForce RTX 4090
`
	t1 := time.Now()
	m1, cpu1, net1 := parseMetricsOutput("s1", sample1, 35, nil, nil, time.Time{}, t1)
	if !m1.Supported || m1.LatencyMs != 35 || m1.MemTotalMB != 8192 || m1.MemUsedMB != 6144 {
		t.Fatalf("unexpected m1: %+v", m1)
	}
	if math.Abs(m1.MemPercent-75.0) > 0.1 || m1.DiskMount != "/data" || m1.DiskPercent != 88 {
		t.Fatalf("unexpected mem/disk in m1: %+v", m1)
	}
	if !m1.HasGPU || m1.GPUUtil != 65 || m1.Cores != 4 || math.Abs(m1.Load1-1.25) > 0.01 {
		t.Fatalf("unexpected gpu/load in m1: %+v", m1)
	}

	sample2 := `---CPU---
cpu  1250 0 750 9000 0 0 0 0
---MEM---
MemTotal: 8388608
MemAvailable: 2097152
---DISK---
42% /
---NET---
2097152 1048576
---LOAD---
1.50 1.00 0.85 1/200 1235
4
---GPU---
`
	t2 := t1.Add(2 * time.Second)
	m2, _, _ := parseMetricsOutput("s1", sample2, 22, cpu1, net1, t1, t2)
	// dTotal = 1000, dIdle = 500 -> 50%
	if math.Abs(m2.CPUPercent-50.0) > 0.1 {
		t.Fatalf("expected 50%% CPU, got %.2f", m2.CPUPercent)
	}
	// rx delta = 1024 KB / 2s = 512 KB/s; tx delta = 512 KB / 2s = 256 KB/s
	if math.Abs(m2.NetRxKBps-512.0) > 0.5 || math.Abs(m2.NetTxKBps-256.0) > 0.5 {
		t.Fatalf("unexpected net speed: rx=%.1f tx=%.1f", m2.NetRxKBps, m2.NetTxKBps)
	}

	unsupported, _, _ := parseMetricsOutput("s2", "UNSUPPORTED\n", 10, nil, nil, time.Time{}, t2)
	if unsupported.Supported {
		t.Fatal("expected UNSUPPORTED output to set Supported=false")
	}

	// Guard uint64 underflow when curCPU.idle < prevCPU.idle and preserve spaces in disk mountpaths.
	prev := &cpuSample{total: 1000, idle: 500}
	spaceDiskAndIdleDrop := `---CPU---
cpu  700 0 400 400 0 0 0 0
---DISK---
91% /mnt/My External Drive
`
	m3, _, _ := parseMetricsOutput("s3", spaceDiskAndIdleDrop, 15, prev, nil, t1, t2)
	if math.Abs(m3.CPUPercent-100.0) > 0.1 {
		t.Fatalf("expected 100%% CPU when idle decreased (no uint64 underflow), got %.2f", m3.CPUPercent)
	}
	if m3.DiskMount != "/mnt/My External Drive" || m3.DiskPercent != 91 {
		t.Fatalf("expected disk mount with spaces preserved, got mount=%q pct=%.1f", m3.DiskMount, m3.DiskPercent)
	}
}

func TestBuildLinuxMetricsCmdCaching(t *testing.T) {
	first := buildLinuxMetricsCmd(true, false, false)
	if !strings.Contains(first, "df -Pkl") || !strings.Contains(first, "nproc") || !strings.Contains(first, "command -v nvidia-smi") {
		t.Fatalf("expected first cmd to include df -Pkl, nproc, command -v nvidia-smi; got:\n%s", first)
	}

	noGPU := buildLinuxMetricsCmd(false, true, false)
	if strings.Contains(noGPU, "nproc") || strings.Contains(noGPU, "nvidia-smi") || strings.Contains(noGPU, "---GPU---") {
		t.Fatalf("expected non-GPU cached cmd to skip nproc and nvidia-smi; got:\n%s", noGPU)
	}

	withGPU := buildLinuxMetricsCmd(false, true, true)
	if strings.Contains(withGPU, "nproc") || strings.Contains(withGPU, "command -v nvidia-smi") || !strings.Contains(withGPU, "---GPU---") {
		t.Fatalf("expected GPU cached cmd to query nvidia-smi directly without nproc/command -v; got:\n%s", withGPU)
	}
}

func TestGetSessionMetricsTrueRTTAndStaticCache(t *testing.T) {
	var (
		mu   sync.Mutex
		cmds []string
	)
	cli := startMockSSHSFTPServer(t, &mFS{m: map[string]*mNode{}}, func(cmd string) (string, uint32) {
		mu.Lock()
		cmds = append(cmds, cmd)
		mu.Unlock()
		time.Sleep(130 * time.Millisecond)
		return `---CPU---
cpu  1000 0 500 8500 0 0 0 0
---MEM---
MemTotal: 4194304
MemAvailable: 2097152
---DISK---
30% /
---NET---
1024 2048
---LOAD---
0.50 0.40 0.30 1/100 42
4
---GPU---
`, 0
	})

	m := NewManager(nil, nil)
	s := &sessionState{
		dialRes:  &DialResult{Client: cli},
		stopCh:   make(chan struct{}),
		dirCache: map[string]dirCacheEntry{},
	}
	m.sessions["s1"] = s

	m1, err := m.GetSessionMetrics("s1")
	if err != nil {
		t.Fatalf("first GetSessionMetrics failed: %v", err)
	}
	if !m1.Supported || m1.Cores != 4 || m1.HasGPU {
		t.Fatalf("unexpected first metrics: %+v", m1)
	}
	// True single RTT via keepalive must NOT include the 130ms exec delay.
	if m1.LatencyMs >= 100 {
		t.Fatalf("expected true single RTT < 100ms (got %dms)", m1.LatencyMs)
	}

	m2, err := m.GetSessionMetrics("s1")
	if err != nil {
		t.Fatalf("second GetSessionMetrics failed: %v", err)
	}
	if m2.Cores != 4 {
		t.Fatalf("expected cached Cores=4 on second sample, got %d", m2.Cores)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(cmds) != 2 {
		t.Fatalf("expected 2 exec commands, got %d", len(cmds))
	}
	if !strings.Contains(cmds[0], "nproc") || !strings.Contains(cmds[0], "nvidia-smi") {
		t.Fatalf("expected first cmd to probe nproc and nvidia-smi")
	}
	if strings.Contains(cmds[1], "nproc") || strings.Contains(cmds[1], "nvidia-smi") {
		t.Fatalf("expected second cmd to skip cached nproc and nvidia-smi, got:\n%s", cmds[1])
	}
}

func TestBuildLinuxMetricsCmdExcludesSnapLoopAndSupportsAlpineFallback(t *testing.T) {
	cmd := buildLinuxMetricsCmd(false, true, false)
	for _, want := range []string{
		"env LC_ALL=C sh -c '",
		"(df -Pkl -x squashfs -x tmpfs -x devtmpfs -x overlay 2>/dev/null || df -Pk 2>/dev/null)",
		`\$1 !~ /^\/dev\/loop/`,
		`\$6 !~ /^\/snap(\/|\$)/`,
	} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("expected metrics cmd to contain %q, got:\n%s", want, cmd)
		}
	}
}
