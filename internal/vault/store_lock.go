package vault

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

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
	defer crypto.ZeroBytes(plain)

	var loaded models.VaultData
	if err := json.Unmarshal(plain, &loaded); err != nil {
		crypto.ZeroBytes(pwKey)
		crypto.ZeroBytes(pwSalt)
		return fmt.Errorf("解析配置文件失败: %w", err)
	}
	normalizeVault(&loaded)
	crypto.ZeroBytes(s.pwKey)
	crypto.ZeroBytes(s.pwSalt)
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
		crypto.ZeroBytes(checkKey)
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
		crypto.ZeroBytes(newKey)
		crypto.ZeroBytes(newSalt)
		return err
	}
	crypto.ZeroBytes(prevKey)
	crypto.ZeroBytes(prevSalt)
	return nil
}

// BindSSHKeyToServer attaches keyID to a saved server and switches authType to key/both
// while preserving any existing passwords.
func (s *Store) BindSSHKeyToServer(serverID, keyID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked {
		return errors.New("配置文件已锁定")
	}
	for i := range s.data.Servers {
		if s.data.Servers[i].ID == serverID {
			prev := s.data.Servers[i]
			s.data.Servers[i].SSHKeyID = keyID
			if s.data.Servers[i].Password != "" {
				s.data.Servers[i].AuthType = models.AuthBoth
			} else {
				s.data.Servers[i].AuthType = models.AuthKey
			}
			s.data.Servers[i].UpdatedAt = time.Now()
			if err := s.saveLocked(); err != nil {
				s.data.Servers[i] = prev
				return err
			}
			return nil
		}
	}
	return fmt.Errorf("服务器不存在: %s", serverID)
}
