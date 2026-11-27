package vault

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/yellowdking/GravitySSH/internal/models"
	"github.com/yellowdking/GravitySSH/internal/sshkey"
)

func (s *Store) ListSSHKeys() []models.SSHKeyMeta {
	s.mu.RLock()
	defer s.mu.RUnlock()

	usage := make(map[string]int)
	for _, srv := range s.data.Servers {
		if srv.SSHKeyID != "" {
			usage[srv.SSHKeyID]++
		}
		if srv.JumpHost.SSHKeyID != "" {
			usage[srv.JumpHost.SSHKeyID]++
		}
	}

	out := make([]models.SSHKeyMeta, 0, len(s.data.SSHKeys))
	for _, k := range s.data.SSHKeys {
		out = append(out, keyMetaFromRecord(k, usage[k.ID]))
	}
	return out
}

func (s *Store) GetSSHKeyInternal(id string) (*models.SSHKeyRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, k := range s.data.SSHKeys {
		if k.ID == id {
			cp := k
			return &cp, nil
		}
	}
	return nil, fmt.Errorf("SSH 密钥不存在: %s", id)
}

func (s *Store) ImportSSHKey(name, pemContent, passphrase, comment string) (models.SSHKeyMeta, error) {
	if s.IsLocked() {
		return models.SSHKeyMeta{}, errVaultLocked
	}
	parsed, err := sshkey.ParsePrivateKey(pemContent, passphrase)
	if err != nil {
		return models.SSHKeyMeta{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = fmt.Sprintf("%s-%s", parsed.KeyType, time.Now().Format("0102-1504"))
	}

	rec := models.SSHKeyRecord{
		ID:            uuid.NewString(),
		Name:          name,
		KeyType:       parsed.KeyType,
		Fingerprint:   parsed.Fingerprint,
		PublicKey:     parsed.PublicKey,
		PrivateKeyPEM: strings.TrimSpace(pemContent),
		Passphrase:    passphrase,
		Comment:       sshkey.SanitizeComment(comment),
		CreatedAt:     time.Now(),
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked {
		return models.SSHKeyMeta{}, errVaultLocked
	}
	snapshot := cloneVaultData(s.data)
	s.data.SSHKeys = append(s.data.SSHKeys, rec)
	if err := s.saveLocked(); err != nil {
		s.data = snapshot
		return models.SSHKeyMeta{}, err
	}
	return keyMetaFromRecord(rec, 0), nil
}

func (s *Store) GenerateSSHKey(name, algorithm, passphrase, comment string) (models.SSHKeyMeta, error) {
	if s.IsLocked() {
		return models.SSHKeyMeta{}, errVaultLocked
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = fmt.Sprintf("GravitySSH-%s", time.Now().Format("20060102-1504"))
	}
	comment = sshkey.SanitizeComment(comment)
	if comment == "" {
		comment = sshkey.SanitizeComment(name)
	}

	pemStr, parsed, err := sshkey.GenerateKeyPair(algorithm, comment, passphrase)
	if err != nil {
		return models.SSHKeyMeta{}, err
	}

	rec := models.SSHKeyRecord{
		ID:            uuid.NewString(),
		Name:          name,
		KeyType:       parsed.KeyType,
		Fingerprint:   parsed.Fingerprint,
		PublicKey:     parsed.PublicKey,
		PrivateKeyPEM: pemStr,
		Passphrase:    passphrase,
		Comment:       comment,
		CreatedAt:     time.Now(),
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked {
		return models.SSHKeyMeta{}, errVaultLocked
	}
	snapshot := cloneVaultData(s.data)
	s.data.SSHKeys = append(s.data.SSHKeys, rec)
	if err := s.saveLocked(); err != nil {
		s.data = snapshot
		return models.SSHKeyMeta{}, err
	}
	return keyMetaFromRecord(rec, 0), nil
}

func keyMetaFromRecord(rec models.SSHKeyRecord, usageCount int) models.SSHKeyMeta {
	return models.SSHKeyMeta{
		ID:            rec.ID,
		Name:          rec.Name,
		KeyType:       rec.KeyType,
		Fingerprint:   rec.Fingerprint,
		PublicKey:     rec.PublicKey,
		HasPassphrase: rec.Passphrase != "",
		Comment:       rec.Comment,
		UsageCount:    usageCount,
		CreatedAt:     rec.CreatedAt,
	}
}

func (s *Store) DeleteSSHKey(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.locked {
		return errVaultLocked
	}
	for i, k := range s.data.SSHKeys {
		if k.ID == id {
			snapshot := cloneVaultData(s.data)
			s.data.SSHKeys = append(s.data.SSHKeys[:i], s.data.SSHKeys[i+1:]...)
			for j := range s.data.Servers {
				if s.data.Servers[j].SSHKeyID == id {
					s.data.Servers[j].SSHKeyID = ""
				}
				if s.data.Servers[j].JumpHost.SSHKeyID == id {
					s.data.Servers[j].JumpHost.SSHKeyID = ""
				}
			}
			if err := s.saveLocked(); err != nil {
				s.data = snapshot
				return err
			}
			return nil
		}
	}
	return nil
}
