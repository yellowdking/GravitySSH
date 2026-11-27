package vault

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/yellowdking/GravitySSH/internal/crypto"
	"github.com/yellowdking/GravitySSH/internal/models"
)

func (s *Store) LockState() models.VaultLockState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ver := s.data.Version
	if ver <= 0 {
		ver = vaultSchemaVersion
	}
	return models.VaultLockState{
		Locked:        s.locked,
		HasPassword:   s.hasPassword,
		VaultPath:     s.filePath,
		SchemaVersion: ver,
	}
}

func (s *Store) IsLocked() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.locked
}

// Unlock decrypts a password-protected vault into memory using the user's password.
func (s *Store) Unlock(password string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.locked {
		return nil
	}
	if password == "" {
		return errors.New("请输入配置解锁密码")
	}

	raw, err := os.ReadFile(s.filePath)
	if err != nil {
		return fmt.Errorf("读取配置文件失败: %w", err)
	}
	plain, pwSalt, pwKey, err := s.enc.OpenWithPasswordAndKey(raw, password)
	if err != nil {
		if errors.Is(err, crypto.ErrWrongPassword) {
			return errors.New("解锁密码不正确，请重试")
		}
		return fmt.Errorf("解密配置文件失败: %w", err)
	}
	defer clear(plain)

	var loaded models.VaultData
	if err := json.Unmarshal(plain, &loaded); err != nil {
		clear(pwKey)
		clear(pwSalt)
		return fmt.Errorf("解析配置文件失败: %w", err)
	}
	normalizeVault(&loaded)
	clear(s.pwKey)
	clear(s.pwSalt)
	s.data = loaded
	s.pwSalt = pwSalt
	s.pwKey = pwKey
	s.hasPassword = true
	s.locked = false
	return nil
}

// SetPassword sets, changes, or removes (when newPassword == "") the custom vault encryption password.
func (s *Store) SetPassword(currentPassword, newPassword string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked {
		return errors.New("请先解锁配置文件后再修改密码")
	}

	if s.hasPassword {
		if currentPassword == "" || len(s.pwSalt) == 0 || len(s.pwKey) == 0 {
			return errors.New("当前密码不正确")
		}
		checkKey, _, err := crypto.DerivePasswordKey(currentPassword, s.pwSalt)
		if err != nil {
			return errors.New("当前密码不正确")
		}
		match := subtle.ConstantTimeCompare(checkKey, s.pwKey) == 1
		clear(checkKey)
		if !match {
			return errors.New("当前密码不正确")
		}
	}

	var newKey, newSalt []byte
	if newPassword != "" {
		var err error
		newKey, newSalt, err = crypto.DerivePasswordKey(newPassword, nil)
		if err != nil {
			return err
		}
	}

	prevSalt := s.pwSalt
	prevKey := s.pwKey
	prevHasPassword := s.hasPassword

	s.pwSalt = newSalt
	s.pwKey = newKey
	s.hasPassword = newPassword != ""

	if err := s.saveLocked(); err != nil {
		s.pwSalt = prevSalt
		s.pwKey = prevKey
		s.hasPassword = prevHasPassword
		clear(newKey)
		clear(newSalt)
		return err
	}
	clear(prevKey)
	clear(prevSalt)
	return nil
}
