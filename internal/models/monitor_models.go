package models

import "time"

type HostMetrics struct {
	SessionID   string    `json:"sessionId"`
	Supported   bool      `json:"supported"`
	LatencyMs   int64     `json:"latencyMs"`
	CPUPercent  float64   `json:"cpuPercent"`
	MemUsedMB   int64     `json:"memUsedMb"`
	MemTotalMB  int64     `json:"memTotalMb"`
	MemPercent  float64   `json:"memPercent"`
	DiskMount   string    `json:"diskMount"`
	DiskPercent float64   `json:"diskPercent"`
	NetRxKBps   float64   `json:"netRxKbps"`
	NetTxKBps   float64   `json:"netTxKbps"`
	Load1       float64   `json:"load1"`
	Cores       int       `json:"cores"`
	HasGPU      bool      `json:"hasGpu"`
	GPUUtil     float64   `json:"gpuUtil"`
	GPUName     string    `json:"gpuName,omitempty"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type UpdateCheckResult struct {
	CurrentVersion string `json:"currentVersion"`
	LatestVersion  string `json:"latestVersion"`
	HasUpdate      bool   `json:"hasUpdate"`
	ReleaseURL     string `json:"releaseUrl"`
	DownloadURL    string `json:"downloadUrl"`
	ReleaseNotes   string `json:"releaseNotes"`
	PublishedAt    string `json:"publishedAt"`
}

type RemoteFileContent struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Size    int64  `json:"size"`
}

type BuiltinTemplate struct {
	ID          string `json:"id"`
	PresetID    string `json:"presetId,omitempty"`
	Name        string `json:"name"`
	Category    string `json:"category"`
	Description string `json:"description"`
	SafetyNote  string `json:"safetyNote"`
	Command     string `json:"command"`
}

type PresetLoadResult struct {
	Added     int               `json:"added"`
	Skipped   int               `json:"skipped"`
	Templates []BuiltinTemplate `json:"templates"`
}
