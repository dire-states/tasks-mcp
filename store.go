package main

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// Task is a single task persisted in the JSON file.
type Task struct {
	ID          int       `json:"id" jsonschema:"unique task ID"`
	Title       string    `json:"title" jsonschema:"short task title"`
	Description string    `json:"description" jsonschema:"longer task description"`
	CreatedAt   time.Time `json:"created_at" jsonschema:"creation time (RFC 3339, UTC)"`
	ModifiedAt  time.Time `json:"modified_at" jsonschema:"last modification time (RFC 3339, UTC)"`
}

var (
	// ErrNotFound is returned when no task has the requested ID.
	ErrNotFound = errors.New("task not found")
	// ErrEmptyTitle is returned when a title is empty or whitespace-only.
	ErrEmptyTitle = errors.New("title must not be empty")
)

// fileData is the on-disk layout of the tasks file.
type fileData struct {
	NextID int    `json:"next_id"`
	Tasks  []Task `json:"tasks"`
}

// Store reads and writes tasks in a JSON file. The file is the source of
// truth: every operation takes an advisory lock on <path>.lock, reloads the
// file and, for writes, replaces it atomically. Several processes can
// therefore share one file safely. Locking uses flock(2) on Unix and
// LockFileEx on Windows (lock_unix.go, lock_windows.go); OpenStore fails on
// platforms with neither.
type Store struct {
	path string
	lock *os.File         // <path>.lock; only ever used as the flock target
	now  func() time.Time // clock; overridden in tests

	mu sync.Mutex // serialises goroutines in this process (flock is per file description)
}

// OpenStore prepares a store for the tasks file at path and validates the
// file if it already exists. A missing or empty file yields an empty store;
// the file is created on the first write.
func OpenStore(path string) (*Store, error) {
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	// Probe the lock once so an unsupported platform or filesystem fails
	// here rather than on the first operation.
	if err := lockFile(lock, lockExclusive); err != nil {
		lock.Close()
		return nil, fmt.Errorf("lock %s: %w", lock.Name(), err)
	}
	unlockFile(lock)
	if _, err := readFile(path); err != nil {
		lock.Close()
		return nil, err
	}
	return &Store{
		path: path,
		lock: lock,
		now:  func() time.Time { return time.Now().UTC() },
	}, nil
}

// Close releases the lock file. The store must not be used afterwards.
func (s *Store) Close() error {
	return s.lock.Close()
}

// readFile parses the tasks file at path. A missing or empty file yields an
// empty fileData. Hand-edited files are repaired where possible (stale or
// zero next_id, tasks out of order) and rejected where not (invalid or
// duplicate IDs).
func readFile(path string) (fileData, error) {
	data := fileData{NextID: 1, Tasks: []Task{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) || (err == nil && len(bytes.TrimSpace(b)) == 0) {
		return data, nil
	}
	if err != nil {
		return fileData{}, err
	}
	if err := json.Unmarshal(b, &data); err != nil {
		return fileData{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if data.Tasks == nil {
		data.Tasks = []Task{}
	}
	seen := make(map[int]bool, len(data.Tasks))
	for _, t := range data.Tasks {
		if t.ID < 1 {
			return fileData{}, fmt.Errorf("parse %s: invalid task id %d", path, t.ID)
		}
		if seen[t.ID] {
			return fileData{}, fmt.Errorf("parse %s: duplicate task id %d", path, t.ID)
		}
		seen[t.ID] = true
		if t.ID >= data.NextID {
			data.NextID = t.ID + 1
		}
	}
	if data.NextID < 1 {
		data.NextID = 1
	}
	slices.SortFunc(data.Tasks, func(a, b Task) int { return cmp.Compare(a.ID, b.ID) })
	return data, nil
}

// save writes data to the tasks file atomically: it is written and synced to
// a temp file in the same directory, then renamed over the real file, so a
// crash or error never leaves a partial file behind. The rename is retried
// on Windows, where another process holding the file open makes it fail
// transiently. Callers must hold the exclusive lock.
func (s *Store) save(data fileData) (err error) {
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.path), "."+filepath.Base(s.path)+".*.tmp")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			os.Remove(f.Name())
		}
	}()
	// CreateTemp uses 0600; match the mode a plain file would have. (A no-op
	// on Windows, which has no mode bits.)
	if err = f.Chmod(0o644); err != nil {
		f.Close()
		return err
	}
	if _, err = f.Write(append(b, '\n')); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return renameFile(f.Name(), s.path)
}

// load takes the file lock (how is lockShared or lockExclusive) and reads the
// file. The returned func releases the lock; callers must invoke it.
func (s *Store) load(how int) (fileData, func(), error) {
	s.mu.Lock()
	if err := lockFile(s.lock, how); err != nil {
		s.mu.Unlock()
		return fileData{}, nil, fmt.Errorf("lock %s: %w", s.lock.Name(), err)
	}
	unlock := func() {
		unlockFile(s.lock)
		s.mu.Unlock()
	}
	data, err := readFile(s.path)
	if err != nil {
		unlock()
		return fileData{}, nil, err
	}
	return data, unlock, nil
}

// indexOf returns the index of the task with id in tasks, or -1.
func indexOf(tasks []Task, id int) int {
	return slices.IndexFunc(tasks, func(t Task) bool { return t.ID == id })
}

// Create adds a task with a fresh ID. The title is trimmed and must not be
// blank.
func (s *Store) Create(title, description string) (Task, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return Task{}, ErrEmptyTitle
	}
	data, unlock, err := s.load(lockExclusive)
	if err != nil {
		return Task{}, err
	}
	defer unlock()

	now := s.now()
	t := Task{
		ID:          data.NextID,
		Title:       title,
		Description: description,
		CreatedAt:   now,
		ModifiedAt:  now,
	}
	data.NextID = t.ID + 1
	data.Tasks = append(data.Tasks, t)
	if err := s.save(data); err != nil {
		return Task{}, fmt.Errorf("save tasks: %w", err)
	}
	return t, nil
}

// List returns all tasks ordered by ID.
func (s *Store) List() ([]Task, error) {
	data, unlock, err := s.load(lockShared)
	if err != nil {
		return nil, err
	}
	unlock()
	return data.Tasks, nil
}

// Get returns the task with the given ID.
func (s *Store) Get(id int) (Task, error) {
	data, unlock, err := s.load(lockShared)
	if err != nil {
		return Task{}, err
	}
	unlock()
	i := indexOf(data.Tasks, id)
	if i < 0 {
		return Task{}, fmt.Errorf("%w: %d", ErrNotFound, id)
	}
	return data.Tasks[i], nil
}

// Update changes the fields that are non-nil and bumps ModifiedAt. A new
// title is trimmed and must not be blank. With neither field given the task
// is returned unchanged and nothing is written.
func (s *Store) Update(id int, title, description *string) (Task, error) {
	if title != nil {
		trimmed := strings.TrimSpace(*title)
		if trimmed == "" {
			return Task{}, ErrEmptyTitle
		}
		title = &trimmed
	}
	data, unlock, err := s.load(lockExclusive)
	if err != nil {
		return Task{}, err
	}
	defer unlock()

	i := indexOf(data.Tasks, id)
	if i < 0 {
		return Task{}, fmt.Errorf("%w: %d", ErrNotFound, id)
	}
	if title == nil && description == nil {
		return data.Tasks[i], nil
	}
	t := data.Tasks[i]
	if title != nil {
		t.Title = *title
	}
	if description != nil {
		t.Description = *description
	}
	t.ModifiedAt = s.now()
	data.Tasks[i] = t
	if err := s.save(data); err != nil {
		return Task{}, fmt.Errorf("save tasks: %w", err)
	}
	return t, nil
}

// Delete removes the task with the given ID. IDs are never reused.
func (s *Store) Delete(id int) error {
	data, unlock, err := s.load(lockExclusive)
	if err != nil {
		return err
	}
	defer unlock()

	i := indexOf(data.Tasks, id)
	if i < 0 {
		return fmt.Errorf("%w: %d", ErrNotFound, id)
	}
	data.Tasks = slices.Delete(data.Tasks, i, i+1)
	if err := s.save(data); err != nil {
		return fmt.Errorf("save tasks: %w", err)
	}
	return nil
}

// Search returns tasks whose title or description contains query,
// case-insensitively. An empty query matches every task.
func (s *Store) Search(query string) ([]Task, error) {
	q := strings.ToLower(query)
	data, unlock, err := s.load(lockShared)
	if err != nil {
		return nil, err
	}
	unlock()

	out := []Task{}
	for _, t := range data.Tasks {
		if strings.Contains(strings.ToLower(t.Title), q) ||
			strings.Contains(strings.ToLower(t.Description), q) {
			out = append(out, t)
		}
	}
	return out, nil
}
