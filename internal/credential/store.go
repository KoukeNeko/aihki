package credential

import (
	"encoding/json"
	"errors"
	"fmt"

	keyring "github.com/zalando/go-keyring"
)

const serviceName = "taiga-cli"

var ErrNotFound = errors.New("credential not found")

type Tokens struct {
	AuthToken    string `json:"auth_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
}

type Store interface {
	Get(account string) (Tokens, error)
	Set(account string, tokens Tokens) error
	Delete(account string) error
}

type backend interface {
	Get(service, account string) (string, error)
	Set(service, account, value string) error
	Delete(service, account string) error
}

type osBackend struct{}

func (osBackend) Get(service, account string) (string, error) {
	return keyring.Get(service, account)
}

func (osBackend) Set(service, account, value string) error {
	return keyring.Set(service, account, value)
}

func (osBackend) Delete(service, account string) error {
	return keyring.Delete(service, account)
}

type KeyringStore struct {
	backend backend
}

func NewKeyringStore() *KeyringStore { return &KeyringStore{backend: osBackend{}} }

func newKeyringStore(backend backend) *KeyringStore { return &KeyringStore{backend: backend} }

func Account(profile, apiURL string) string { return profile + "|" + apiURL }

func (s *KeyringStore) Get(account string) (Tokens, error) {
	value, err := s.backend.Get(serviceName, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return Tokens{}, ErrNotFound
	}
	if err != nil {
		return Tokens{}, fmt.Errorf("read OS keyring: %w", err)
	}
	var tokens Tokens
	if err := json.Unmarshal([]byte(value), &tokens); err != nil {
		return Tokens{}, fmt.Errorf("decode OS keyring entry: %w", err)
	}
	return tokens, nil
}

func (s *KeyringStore) Set(account string, tokens Tokens) error {
	data, err := json.Marshal(tokens)
	if err != nil {
		return fmt.Errorf("encode credential: %w", err)
	}
	if err := s.backend.Set(serviceName, account, string(data)); err != nil {
		return fmt.Errorf("write OS keyring: %w", err)
	}
	return nil
}

func (s *KeyringStore) Delete(account string) error {
	err := s.backend.Delete(serviceName, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("delete OS keyring entry: %w", err)
	}
	return nil
}
