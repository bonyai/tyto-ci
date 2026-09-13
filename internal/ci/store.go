package ci

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// Store is deliberately small so production deployments can replace the file
// implementation with Postgres without changing scheduling behavior.
type Store interface {
	Load() ([]JobRecord, error)
	Put(JobRecord) error
	Delete(jobID string) error
}

type MemoryStore struct {
	mu      sync.Mutex
	records map[string]JobRecord
}

func NewMemoryStore() *MemoryStore { return &MemoryStore{records: make(map[string]JobRecord)} }

func (s *MemoryStore) Load() ([]JobRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]JobRecord, 0, len(s.records))
	for _, record := range s.records {
		result = append(result, record)
	}
	return result, nil
}
func (s *MemoryStore) Put(record JobRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[record.Job.ID] = record
	return nil
}
func (s *MemoryStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.records, id)
	return nil
}

// FileStore provides crash-safe local durability for a single service
// instance. Multiple replicas must use a transactional shared Store instead.
type FileStore struct {
	mu   sync.Mutex
	path string
}

func NewFileStore(path string) (*FileStore, error) {
	if path == "" {
		return nil, errors.New("state file path is required")
	}
	return &FileStore{path: path}, nil
}
func (s *FileStore) Load() ([]JobRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked()
}
func (s *FileStore) Put(record JobRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	records, err := s.loadLocked()
	if err != nil {
		return err
	}
	updated := false
	for i := range records {
		if records[i].Job.ID == record.Job.ID {
			records[i] = record
			updated = true
		}
	}
	if !updated {
		records = append(records, record)
	}
	return s.saveLocked(records)
}
func (s *FileStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	records, err := s.loadLocked()
	if err != nil {
		return err
	}
	kept := records[:0]
	for _, record := range records {
		if record.Job.ID != id {
			kept = append(kept, record)
		}
	}
	return s.saveLocked(kept)
}
func (s *FileStore) loadLocked() ([]JobRecord, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var records []JobRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, err
	}
	return records, nil
}
func (s *FileStore) saveLocked(records []JobRecord) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	data, err := json.Marshal(records)
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
