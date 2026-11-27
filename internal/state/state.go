package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const logLimit = 200

// Token is one provider's OAuth tokens.
type Token struct {
	AccessToken  string    `json:"accessToken"`
	RefreshToken string    `json:"refreshToken,omitempty"`
	ExpiresAt    time.Time `json:"expiresAt"`
}

// Connected reports whether a token is stored.
func (t Token) Connected() bool { return t.AccessToken != "" }

// Expired reports whether the access token is close enough to expiry to renew.
func (t Token) Expired() bool { return time.Now().After(t.ExpiresAt.Add(-time.Minute)) }

// Data is everything the daemon keeps between runs.
type Data struct {
	Google   Token             `json:"google"`
	Garmin   Token             `json:"garmin"`
	GarminAs string            `json:"garminAs,omitempty"`
	Uploaded map[string]string `json:"uploaded"`
	LastSync time.Time         `json:"lastSync"`
	Log      []string          `json:"log,omitempty"`
}

// Store serialises access to the state file.
type Store struct {
	mu   sync.Mutex
	path string
	data Data
}

// Open loads the state file, treating a missing file as an empty state.
func Open(path string) (*Store, error) {
	store := &Store{path: path, data: Data{Uploaded: map[string]string{}}}

	raw, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return store, nil
	case err != nil:
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	err = json.Unmarshal(raw, &store.data)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if store.data.Uploaded == nil {
		store.data.Uploaded = map[string]string{}
	}

	return store, nil
}

// Update applies fn under the lock and writes the file.
func (s *Store) Update(fn func(*Data) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	err := fn(&s.data)
	if err != nil {
		return fmt.Errorf("apply state update: %w", err)
	}

	return s.write()
}

// View returns a copy of the state for reading. Do not modify its maps or slices.
func (s *Store) View() Data {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.data
}

// Logf records one line, keeping only the newest lines.
func (s *Store) Logf(format string, args ...any) {
	line := time.Now().Format("2006-01-02 15:04") + " " + fmt.Sprintf(format, args...)

	_ = s.Update(func(data *Data) error {
		data.Log = append(data.Log, line)
		if len(data.Log) > logLimit {
			data.Log = data.Log[len(data.Log)-logLimit:]
		}

		return nil
	})
}

func (s *Store) write() error {
	raw, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}

	directory := filepath.Dir(s.path)

	err = os.MkdirAll(directory, 0o700)
	if err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}

	tmp := s.path + ".tmp"

	err = os.WriteFile(tmp, raw, 0o600)
	if err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}

	return os.Rename(tmp, s.path)
}
