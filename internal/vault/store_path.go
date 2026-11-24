package vault

import (
	"fmt"
	"os"
	"path/filepath"
)

// DefaultVaultPath resolves the portable vault path next to the running executable:
//
//	<exeDir>/config/vault.gsv
//
// If that file does not exist yet and a legacy %APPDATA%/GravitySSH/vault.gsv exists,
// it is automatically copied to the portable location. If <exeDir>/config is not writable,
// it falls back cleanly to %APPDATA%/GravitySSH/config/vault.gsv.
func DefaultVaultPath() (string, error) {
	exePath, exeErr := os.Executable()
	exeDir := ""
	if exeErr == nil && exePath != "" {
		if resolved, err := filepath.EvalSymlinks(exePath); err == nil {
			exeDir = filepath.Dir(resolved)
		} else {
			exeDir = filepath.Dir(exePath)
		}
	}
	cfgDir, cfgErr := os.UserConfigDir()
	if cfgErr != nil && exeDir == "" {
		return "", fmt.Errorf("resolve config directory: %w", cfgErr)
	}
	return resolveDefaultVaultPath(exeDir, cfgDir)
}

func resolveDefaultVaultPath(exeDir, userConfigDir string) (string, error) {
	legacyPath := ""
	if userConfigDir != "" {
		legacyPath = filepath.Join(userConfigDir, "GravitySSH", "vault.gsv")
	}

	if exeDir != "" {
		portableDir := filepath.Join(exeDir, "config")
		portablePath := filepath.Join(portableDir, "vault.gsv")
		if err := ensureDirWritable(portableDir); err == nil {
			if _, statErr := os.Stat(portablePath); os.IsNotExist(statErr) && legacyPath != "" {
				_ = migrateLegacyVault(legacyPath, portablePath)
			}
			return portablePath, nil
		}
	}

	if userConfigDir == "" {
		return "", fmt.Errorf("portable config dir is not writable and user config dir is unavailable")
	}

	fallbackDir := filepath.Join(userConfigDir, "GravitySSH", "config")
	fallbackPath := filepath.Join(fallbackDir, "vault.gsv")
	if err := os.MkdirAll(fallbackDir, 0o700); err != nil {
		return "", fmt.Errorf("create fallback config dir: %w", err)
	}
	if _, statErr := os.Stat(fallbackPath); os.IsNotExist(statErr) && legacyPath != "" {
		_ = migrateLegacyVault(legacyPath, fallbackPath)
	}
	return fallbackPath, nil
}

func ensureDirWritable(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	probe, err := os.CreateTemp(dir, ".gsv-write-probe-*")
	if err != nil {
		return err
	}
	name := probe.Name()
	_ = probe.Close()
	_ = os.Remove(name)
	return nil
}

func migrateLegacyVault(legacyPath, targetPath string) error {
	raw, err := os.ReadFile(legacyPath)
	if err != nil || len(raw) == 0 {
		return err
	}
	tmpPath := targetPath + ".migrate.tmp"
	if err := os.WriteFile(tmpPath, raw, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, targetPath); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}
