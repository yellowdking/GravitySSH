package sshclient

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/yellowdking/GravitySSH/internal/models"
)

var (
	rttProbeTimeout    = 5 * time.Second
	metricsExecTimeout = 10 * time.Second
)

const (

	linuxMetricsHead = `env LC_ALL=C sh -c '
[ -r /proc/stat ] || { echo "UNSUPPORTED"; exit 0; }
echo "---CPU---"; head -n1 /proc/stat
echo "---MEM---"; awk "/^(MemTotal|MemAvailable|MemFree|Buffers|Cached):/{print \$1,\$2}" /proc/meminfo
echo "---DISK---"; (df -Pkl -x squashfs -x tmpfs -x devtmpfs -x overlay 2>/dev/null || df -Pk 2>/dev/null) | awk "NR>1 && \$1 ~ /^\// && \$1 !~ /^\/dev\/loop/ && \$6 !~ /^\/snap(\/|\$)/ {pct=\$5; mp=\$6; for(i=7;i<=NF;i++) mp=mp \" \" \$i; print pct, mp}"
echo "---NET---"; awk "NR>2 && \$1 !~ /^lo:/ {gsub(/:/,\" \",\$0); rx+=\$2; tx+=\$10} END{printf \"%.0f %.0f\n\",rx,tx}" /proc/net/dev
echo "---LOAD---"; cat /proc/loadavg`
	linuxMetricsCoresSuffix = `; nproc 2>/dev/null || grep -c ^processor /proc/cpuinfo`
	linuxMetricsGPUProbe    = "\necho \"---GPU---\"; command -v nvidia-smi >/dev/null 2>&1 && nvidia-smi --query-gpu=utilization.gpu,name --format=csv,noheader,nounits 2>/dev/null | head -n1 || true"
	linuxMetricsGPUQuery    = "\necho \"---GPU---\"; nvidia-smi --query-gpu=utilization.gpu,name --format=csv,noheader,nounits 2>/dev/null | head -n1 || true"
)

func buildLinuxMetricsCmd(needCores, gpuChecked, hasGPU bool) string {
	var b strings.Builder
	b.Grow(len(linuxMetricsHead) + len(linuxMetricsCoresSuffix) + len(linuxMetricsGPUProbe) + 4)
	b.WriteString(linuxMetricsHead)
	if needCores {
		b.WriteString(linuxMetricsCoresSuffix)
	}
	if !gpuChecked {
		b.WriteString(linuxMetricsGPUProbe)
	} else if hasGPU {
		b.WriteString(linuxMetricsGPUQuery)
	}
	b.WriteString("\n'")
	return b.String()
}

// GetSessionMetrics samples RTT latency and Linux host resources over the active SSH connection.
func (m *Manager) GetSessionMetrics(sessionID string) (models.HostMetrics, error) {
	sess := m.getSession(sessionID)
	if sess == nil {
		return models.HostMetrics{SessionID: sessionID}, fmt.Errorf("会话不存在")
	}

	if !sess.metricsInFlight.CompareAndSwap(false, true) {
		return models.HostMetrics{SessionID: sessionID}, fmt.Errorf("主机指标采样进行中")
	}
	defer sess.metricsInFlight.Store(false)

	sess.mu.Lock()
	var client *DialResult
	if sess.dialRes != nil && sess.dialRes.Client != nil {
		client = sess.dialRes
	}
	stopCh := sess.stopCh
	cachedCores := sess.cachedCores
	gpuChecked := sess.gpuChecked
	hasGPU := sess.hasGPU
	sess.mu.Unlock()
	if client == nil {
		return models.HostMetrics{SessionID: sessionID}, nil
	}

	// Measure true single-RTT via global keepalive request before exec-based metrics sampling.
	// If the probe fails or times out, return early instead of launching a 10s exec on a dead/stalled link.
	pingStart := time.Now()
	pingErr := sendKeepAliveProbe(client.Client, stopCh, rttProbeTimeout, &sess.rttProbeInFlight)
	if pingErr != nil {
		return models.HostMetrics{SessionID: sessionID}, pingErr
	}
	latencyMs := time.Since(pingStart).Milliseconds()

	cmd := buildLinuxMetricsCmd(cachedCores <= 0, gpuChecked, hasGPU)
	execCtx, cancelExec := context.WithCancel(context.Background())
	defer cancelExec()
	if stopCh != nil {
		go func() {
			select {
			case <-stopCh:
				cancelExec()
			case <-execCtx.Done():
			}
		}()
	}
	out, runErr := runSSHExec(execCtx, client.Client, cmd, metricsExecTimeout, false)
	if runErr != nil {
		return models.HostMetrics{SessionID: sessionID, LatencyMs: latencyMs}, runErr
	}

	now := time.Now()
	sess.mu.Lock()
	prevCPU := sess.prevCPU
	prevNet := sess.prevNet
	prevAt := sess.prevSampleAt
	sess.mu.Unlock()

	metrics, curCPU, curNet := parseMetricsOutput(sessionID, string(out), latencyMs, prevCPU, prevNet, prevAt, now)
	if cachedCores > 0 {
		metrics.Cores = cachedCores
	}

	sess.mu.Lock()
	if curCPU != nil {
		sess.prevCPU = curCPU
	}
	if curNet != nil {
		sess.prevNet = curNet
	}
	if metrics.Supported {
		if sess.cachedCores <= 0 && metrics.Cores > 0 {
			sess.cachedCores = metrics.Cores
		}
		if !sess.gpuChecked {
			sess.gpuChecked = true
			sess.hasGPU = metrics.HasGPU
		}
	}
	sess.prevSampleAt = now
	sess.mu.Unlock()

	return metrics, nil
}

func parseMetricsOutput(
	sessionID, raw string,
	latencyMs int64,
	prevCPU *cpuSample,
	prevNet *netSample,
	prevAt, now time.Time,
) (models.HostMetrics, *cpuSample, *netSample) {
	res := models.HostMetrics{
		SessionID: sessionID,
		LatencyMs: latencyMs,
		UpdatedAt: now,
	}
	if strings.Contains(raw, "UNSUPPORTED") {
		return res, nil, nil
	}

	section := ""
	var (
		curCPU *cpuSample
		curNet *netSample
		memMap = map[string]int64{}
		maxPct = -1.0
	)

	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		if strings.HasPrefix(t, "---") && strings.HasSuffix(t, "---") {
			section = strings.Trim(t, "-")
			continue
		}
		switch section {
		case "CPU":
			if sample, ok := parseCPULine(t); ok {
				curCPU = sample
				res.Supported = true
			}
		case "MEM":
			if f := strings.Fields(t); len(f) >= 2 {
				k := strings.TrimSuffix(f[0], ":")
				v, _ := strconv.ParseInt(f[1], 10, 64)
				memMap[k] = v
			}
		case "DISK":
			if pct, mount, ok := parseDiskLine(t); ok && pct >= maxPct {
				maxPct = pct
				res.DiskPercent = pct
				res.DiskMount = mount
			}
		case "NET":
			if f := strings.Fields(t); len(f) >= 2 {
				rx, _ := strconv.ParseUint(f[0], 10, 64)
				tx, _ := strconv.ParseUint(f[1], 10, 64)
				curNet = &netSample{rx: rx, tx: tx}
			}
		case "LOAD":
			f := strings.Fields(t)
			if len(f) >= 3 && res.Load1 == 0 {
				res.Load1, _ = strconv.ParseFloat(f[0], 64)
			} else if len(f) == 1 && res.Cores == 0 {
				res.Cores, _ = strconv.Atoi(f[0])
			}
		case "GPU":
			if parts := strings.SplitN(t, ",", 2); len(parts) == 2 {
				if u, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64); err == nil {
					res.HasGPU = true
					res.GPUUtil = u
					res.GPUName = strings.TrimSpace(parts[1])
				}
			}
		}
	}

	if curCPU != nil && prevCPU != nil && curCPU.total > prevCPU.total {
		res.CPUPercent = computeCPUPercent(curCPU, prevCPU)
	}
	applyMemoryMetrics(&res, memMap)
	applyNetRates(&res, curNet, prevNet, prevAt, now)

	if res.Cores <= 0 {
		res.Cores = 1
	}
	return res, curCPU, curNet
}

func parseCPULine(line string) (*cpuSample, bool) {
	f := strings.Fields(line)
	if len(f) < 5 || f[0] != "cpu" {
		return nil, false
	}
	var total, idle uint64
	for i := 1; i < len(f); i++ {
		v, _ := strconv.ParseUint(f[i], 10, 64)
		total += v
		if i == 4 || i == 5 {
			idle += v
		}
	}
	return &cpuSample{total: total, idle: idle}, true
}

func parseDiskLine(line string) (float64, string, bool) {
	idx := strings.IndexAny(line, " \t")
	if idx <= 0 {
		return 0, "", false
	}
	pctStr := strings.TrimSuffix(line[:idx], "%")
	mount := strings.TrimSpace(line[idx+1:])
	if mount == "" {
		return 0, "", false
	}
	pct, err := strconv.ParseFloat(pctStr, 64)
	if err != nil {
		return 0, "", false
	}
	return pct, mount, true
}

func computeCPUPercent(curCPU, prevCPU *cpuSample) float64 {
	dTotal := float64(curCPU.total - prevCPU.total)
	var dIdle float64
	if curCPU.idle >= prevCPU.idle {
		dIdle = float64(curCPU.idle - prevCPU.idle)
	}
	pct := (1.0 - dIdle/dTotal) * 100.0
	if pct < 0 {
		return 0
	}
	if pct > 100 {
		return 100
	}
	return pct
}

func applyMemoryMetrics(res *models.HostMetrics, memMap map[string]int64) {
	totalKB := memMap["MemTotal"]
	if totalKB <= 0 {
		return
	}
	availKB := memMap["MemAvailable"]
	if availKB <= 0 {
		availKB = memMap["MemFree"] + memMap["Buffers"] + memMap["Cached"]
	}
	usedKB := totalKB - availKB
	if usedKB < 0 {
		usedKB = 0
	}
	res.MemTotalMB = totalKB / 1024
	res.MemUsedMB = usedKB / 1024
	res.MemPercent = float64(usedKB) / float64(totalKB) * 100.0
	res.Supported = true
}

func applyNetRates(res *models.HostMetrics, curNet, prevNet *netSample, prevAt, now time.Time) {
	if curNet == nil || prevNet == nil || prevAt.IsZero() {
		return
	}
	dt := now.Sub(prevAt).Seconds()
	if dt <= 0.2 {
		return
	}
	if curNet.rx >= prevNet.rx {
		res.NetRxKBps = float64(curNet.rx-prevNet.rx) / 1024.0 / dt
	}
	if curNet.tx >= prevNet.tx {
		res.NetTxKBps = float64(curNet.tx-prevNet.tx) / 1024.0 / dt
	}
}
