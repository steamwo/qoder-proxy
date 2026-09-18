package credential

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Credential struct {
	Token                 string   `json:"token"`
	RefreshToken          string   `json:"refresh_token,omitempty"`
	RefreshTokenExpiresAt int64    `json:"refresh_token_expires_at,omitempty"`
	UserID                string   `json:"user_id"`
	MachineID             string   `json:"machine_id"`
	Name                  string   `json:"name,omitempty"`
	Email                 string   `json:"email,omitempty"`
	OrganizationID        string   `json:"organization_id,omitempty"`
	OrganizationTags      []string `json:"organization_tags,omitempty"`
	DataPolicyAgreed      bool     `json:"data_policy_agreed,omitempty"`
	RuntimeProfileVersion int      `json:"runtime_profile_version,omitempty"`
	MemberID              string   `json:"member_id,omitempty"`
	EncryptUserInfo       string   `json:"encrypt_user_info,omitempty"`
	CosyKey               string   `json:"cosy_key,omitempty"`
	ExpiresAt             int64    `json:"expires_at,omitempty"`
	CreatedAt             int64    `json:"created_at"`
}

func (c Credential) Valid() bool {
	return c.Token != "" && c.UserID != "" && c.MachineID != ""
}

func (c Credential) Expired() bool {
	return c.ExpiresAt > 0 && time.Now().Unix() >= c.ExpiresAt
}

type Store struct {
	Path string
}

func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "qoder-proxy", "credentials.json"), nil
}

func New(path string) (*Store, error) {
	if path == "" {
		var err error
		path, err = DefaultPath()
		if err != nil {
			return nil, err
		}
	}
	return &Store{Path: path}, nil
}

func (s *Store) Load() (Credential, error) {
	data, err := os.ReadFile(s.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Credential{}, fmt.Errorf("qoder credential not found; run `qoder-proxy login`")
		}
		return Credential{}, err
	}
	var c Credential
	if err := json.Unmarshal(data, &c); err != nil {
		return Credential{}, fmt.Errorf("decode credential: %w", err)
	}
	if !c.Valid() {
		return Credential{}, fmt.Errorf("stored qoder credential is incomplete; run `qoder-proxy login` again")
	}
	return c, nil
}

func (s *Store) Save(c Credential) error {
	if !c.Valid() {
		return errors.New("refusing to save incomplete qoder credential")
	}
	if c.CreatedAt == 0 {
		c.CreatedAt = time.Now().Unix()
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.Path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil && !errors.Is(err, os.ErrPermission) {
		return err
	}
	return os.Rename(tmp, s.Path)
}

func (s *Store) Delete() error {
	err := os.Remove(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
