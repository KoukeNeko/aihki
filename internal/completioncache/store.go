package completioncache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/KoukeNeko/taiga-cli/internal/atomicfile"
)

const schemaVersion = 1

const (
	FreshTTL = 5 * time.Minute
	StaleTTL = 24 * time.Hour
)

type Entry struct {
	UpdatedAt time.Time `json:"updated_at"`
	Values    []string  `json:"values"`
}

type file struct {
	Version int              `json:"version"`
	Entries map[string]Entry `json:"entries"`
}

type Store struct {
	path string
	now  func() time.Time
	mu   sync.Mutex
}

func NewStore(path string) *Store {
	return &Store{path: path, now: time.Now}
}

func DefaultPath(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), "completion-cache.json")
}

func Key(profile, apiURL, project, kind string) string {
	value := strings.Join([]string{profile, apiURL, project, kind}, "\x00")
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func (s *Store) Get(key string) (values []string, fresh bool, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cache, err := s.load()
	if err != nil {
		return nil, false, false
	}
	entry, ok := cache.Entries[key]
	if !ok {
		return nil, false, false
	}
	age := s.now().Sub(entry.UpdatedAt)
	if age < 0 {
		age = 0
	}
	if age > StaleTTL {
		return nil, false, false
	}
	return append([]string(nil), entry.Values...), age <= FreshTTL, true
}

func (s *Store) Put(key string, values []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cache, err := s.load()
	if err != nil {
		cache = file{Version: schemaVersion, Entries: map[string]Entry{}}
	}
	now := s.now()
	for entryKey, entry := range cache.Entries {
		if now.Sub(entry.UpdatedAt) > StaleTTL {
			delete(cache.Entries, entryKey)
		}
	}
	sorted := append([]string(nil), values...)
	sort.Strings(sorted)
	cache.Entries[key] = Entry{UpdatedAt: now.UTC(), Values: sorted}
	return s.save(cache)
}

func (s *Store) load() (file, error) {
	cache := file{Version: schemaVersion, Entries: map[string]Entry{}}
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return cache, nil
	}
	if err != nil {
		return file{}, fmt.Errorf("read completion cache: %w", err)
	}
	if err := json.Unmarshal(data, &cache); err != nil {
		return file{}, fmt.Errorf("decode completion cache: %w", err)
	}
	if cache.Version != schemaVersion {
		return file{Version: schemaVersion, Entries: map[string]Entry{}}, nil
	}
	if cache.Entries == nil {
		cache.Entries = map[string]Entry{}
	}
	return cache, nil
}

func (s *Store) save(cache file) error {
	data, err := json.Marshal(cache)
	if err != nil {
		return fmt.Errorf("encode completion cache: %w", err)
	}
	if err := atomicfile.Write(s.path, data); err != nil {
		return fmt.Errorf("save completion cache %q: %w", s.path, err)
	}
	return nil
}
