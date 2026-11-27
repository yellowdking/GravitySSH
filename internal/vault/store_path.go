package vault

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
)

const configDirName = "gsshconfig"

// DefaultVaultPath resolves the portable vault path next to the running executable:
//
//	<exeDir>/gsshconfig/vault.gsv
//
// If that file does not exist yet, it automatically migrates from legacy locations
// (<exeDir>/config/vault.gsv or %APPDATA%/GravitySSH/vault.gsv). If <exeDir>/gsshconfig
// is not writable, it falls back cleanly to %APPDATA%/GravitySSH/gsshconfig/vault.gsv.
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
	var legacyCandidates []string
	if exeDir != "" {
		legacyCandidates = append(legacyCandidates, filepath.Join(exeDir, "config", "vault.gsv"))
	}
	if userConfigDir != "" {
		legacyCandidates = append(legacyCandidates,
			filepath.Join(userConfigDir, "GravitySSH", "config", "vault.gsv"),
			filepath.Join(userConfigDir, "GravitySSH", "vault.gsv"),
		)
	}

	if exeDir != "" {
		portableDir := filepath.Join(exeDir, configDirName)
		portablePath := filepath.Join(portableDir, "vault.gsv")
		if err := ensureDirWritable(portableDir); err == nil {
			if err := migrateFirstExisting(legacyCandidates, portablePath); err != nil {
				return "", err
			}
			return portablePath, nil
		}
	}

	if userConfigDir == "" {
		return "", fmt.Errorf("portable config dir is not writable and user config dir is unavailable")
	}

	fallbackDir := filepath.Join(userConfigDir, "GravitySSH", configDirName)
	fallbackPath := filepath.Join(fallbackDir, "vault.gsv")
	if err := os.MkdirAll(fallbackDir, 0o700); err != nil {
		return "", fmt.Errorf("create fallback config dir: %w", err)
	}
	if err := migrateFirstExisting(legacyCandidates, fallbackPath); err != nil {
		return "", err
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

func hasVaultMagic(raw []byte) bool {
	return len(raw) >= 4 && (bytes.Equal(raw[:4], []byte("GSV1")) || bytes.Equal(raw[:4], []byte("GSV2")))
}

func migrateFirstExisting(candidates []string, targetPath string) error {
	if _, err := os.Stat(targetPath); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat vault target %s: %w", targetPath, err)
	}
	for _, src := range candidates {
		if src == "" || src == targetPath {
			continue
		}
		raw, err := os.ReadFile(src)
		if err != nil || !hasVaultMagic(raw) {
			continue
		}
		if err := writeFileAtomic(targetPath, raw); err != nil {
			return fmt.Errorf("migrate legacy vault from %s to %s: %w", src, targetPath, err)
		}
		return nil
	}
	return nil
}
