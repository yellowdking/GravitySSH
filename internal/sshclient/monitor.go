package sshclient

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/yellowdking/GravitySSH/internal/models"
)

const linuxMetricsCmd = `LC_ALL=C sh -c '
[ -r /proc/stat ] || { echo "UNSUPPORTED"; exit 0; }
echo "---CPU---"; head -n1 /proc/stat
echo "---MEM---"; awk "/^(MemTotal|MemAvailable|MemFree|Buffers|Cached):/{print \$1,\$2}" /proc/meminfo
echo "---DISK---"; df -Pk 2>/dev/null | awk "NR>1 && \$1 ~ /^\// {pct=\$5; mp=\$6; for(i=7;i<=NF;i++) mp=mp \" \" \$i; print pct, mp}"
echo "---NET---"; awk "NR>2 && \$1 !~ /^lo:/ {gsub(/:/,\" \",\$0); rx+=\$2; tx+=\$10} END{printf \"%.0f %.0f\n\",rx,tx}" /proc/net/dev
echo "---LOAD---"; cat /proc/loadavg; nproc 2>/dev/null || grep -c ^processor /proc/cpuinfo
echo "---GPU---"; command -v nvidia-smi >/dev/null 2>&1 && nvidia-smi --query-gpu=utilization.gpu,name --format=csv,noheader,nounits 2>/dev/null | head -n1 || true
'`

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
	sess.mu.Unlock()
	if client == nil {
		return models.HostMetrics{SessionID: sessionID}, nil
	}

	start := time.Now()
	out, runErr := runSSHExec(context.Background(), client.Client, linuxMetricsCmd, 10*time.Second, false)
	latencyMs := time.Since(start).Milliseconds()
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

	sess.mu.Lock()
	if curCPU != nil {
		sess.prevCPU = curCPU
	}
	if curNet != nil {
		sess.prevNet = curNet
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
			if f := strings.Fields(t); len(f) >= 5 && f[0] == "cpu" {
				var total, idle uint64
				for i := 1; i < len(f); i++ {
					v, _ := strconv.ParseUint(f[i], 10, 64)
					total += v
					if i == 4 || i == 5 {
						idle += v
					}
				}
				curCPU = &cpuSample{total: total, idle: idle}
				res.Supported = true
			}
		case "MEM":
			if f := strings.Fields(t); len(f) >= 2 {
				k := strings.TrimSuffix(f[0], ":")
				v, _ := strconv.ParseInt(f[1], 10, 64)
				memMap[k] = v
			}
		case "DISK":
			if idx := strings.IndexAny(t, " \t"); idx > 0 {
				pctStr := strings.TrimSuffix(t[:idx], "%")
				mount := strings.TrimSpace(t[idx+1:])
				if mount != "" {
					if pct, err := strconv.ParseFloat(pctStr, 64); err == nil && pct >= maxPct {
						maxPct = pct
						res.DiskPercent = pct
						res.DiskMount = mount
					}
				}
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
		dTotal := float64(curCPU.total - prevCPU.total)
		var dIdle float64
		if curCPU.idle >= prevCPU.idle {
			dIdle = float64(curCPU.idle - prevCPU.idle)
		}
		pct := (1.0 - dIdle/dTotal) * 100.0
		if pct < 0 {
			pct = 0
		} else if pct > 100 {
			pct = 100
		}
		res.CPUPercent = pct
	}

	if totalKB := memMap["MemTotal"]; totalKB > 0 {
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

	if curNet != nil && prevNet != nil && !prevAt.IsZero() {
		dt := now.Sub(prevAt).Seconds()
		if dt > 0.2 {
			if curNet.rx >= prevNet.rx {
				res.NetRxKBps = float64(curNet.rx-prevNet.rx) / 1024.0 / dt
			}
			if curNet.tx >= prevNet.tx {
				res.NetTxKBps = float64(curNet.tx-prevNet.tx) / 1024.0 / dt
			}
		}
	}

	if res.Cores <= 0 {
		res.Cores = 1
	}
	return res, curCPU, curNet
}
