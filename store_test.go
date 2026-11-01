package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// newTestStore opens a store on a fresh temp file and returns it with its path.
func newTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tasks.json")
	return openStore(t, path), path
}

// openStore opens a store at path and closes it when the test ends. Tests
// must open stores through it: the lock file must be closed before TempDir
// is removed, which Windows enforces.
func openStore(t *testing.T, path string) *Store {
	t.Helper()
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// list returns all tasks or fails the test.
func list(t *testing.T, s *Store) []Task {
	t.Helper()
	tasks, err := s.List("")
	if err != nil {
		t.Fatal(err)
	}
	return tasks
}

func TestCRUD(t *testing.T) {
	s, path := newTestStore(t)
	dir := filepath.Dir(path)

	a, err := s.Create("  Buy milk ", "2 litres", "")
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != 1 || a.Title != "Buy milk" || a.Scope != dir || a.CreatedAt.IsZero() || !a.CreatedAt.Equal(a.ModifiedAt) {
		t.Fatalf("unexpected task: %+v", a)
	}
	if _, err := s.Create("  ", "", ""); !errors.Is(err, ErrEmptyTitle) {
		t.Fatalf("want ErrEmptyTitle, got %v", err)
	}

	got, err := s.Get(a.ID)
	if err != nil || got != a {
		t.Fatalf("Get = %+v, %v", got, err)
	}

	// Make the update's timestamp observably later.
	later := a.CreatedAt.Add(time.Minute)
	s.now = func() time.Time { return later }
	u, err := s.Update(a.ID, nil, new("3 litres"), new(" work "))
	if err != nil {
		t.Fatal(err)
	}
	if u.Title != "Buy milk" || u.Description != "3 litres" || u.Scope != "work" || !u.CreatedAt.Equal(a.CreatedAt) || !u.ModifiedAt.Equal(later) {
		t.Fatalf("unexpected update: %+v", u)
	}
	if r, err := s.Update(a.ID, nil, nil, new("")); err != nil || r.Scope != dir {
		t.Fatalf("want scope reset to %q, got %+v, %v", dir, r, err)
	}
	if _, err := s.Update(a.ID, new(""), nil, nil); !errors.Is(err, ErrEmptyTitle) {
		t.Fatalf("want ErrEmptyTitle, got %v", err)
	}

	if err := s.Delete(a.ID); err != nil {
		t.Fatal(err)
	}
	for _, err := range []error{
		s.Delete(a.ID),
		func() error { _, err := s.Get(a.ID); return err }(),
		func() error { _, err := s.Update(a.ID, new("x"), nil, nil); return err }(),
	} {
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("want ErrNotFound, got %v", err)
		}
	}

	// IDs are not reused after delete.
	b, _ := s.Create("Next", "", "")
	if b.ID != 2 {
		t.Fatalf("want ID 2, got %d", b.ID)
	}
}

func TestUpdateNoFields(t *testing.T) {
	s, path := newTestStore(t)
	a, _ := s.Create("x", "", "")
	before, _ := os.ReadFile(path)

	s.now = func() time.Time { return a.CreatedAt.Add(time.Hour) }
	u, err := s.Update(a.ID, nil, nil, nil)
	if err != nil || u != a {
		t.Fatalf("want unchanged task %+v, got %+v, %v", a, u, err)
	}
	if after, _ := os.ReadFile(path); !bytes.Equal(before, after) {
		t.Fatal("file was rewritten by a no-op update")
	}
}

func TestSearch(t *testing.T) {
	s, _ := newTestStore(t)
	s.Create("Write report", "quarterly numbers", "")
	s.Create("Call Bob", "about the REPORT", "")
	s.Create("Groceries", "", "")

	if got, _ := s.Search("report", ""); len(got) != 2 {
		t.Fatalf("want 2 matches, got %+v", got)
	}
	if got, _ := s.Search("", ""); len(got) != 3 {
		t.Fatalf("want empty query to match all 3, got %+v", got)
	}
	if got, _ := s.Search("nothing", ""); got == nil || len(got) != 0 {
		t.Fatalf("want empty non-nil slice, got %#v", got)
	}
}

func TestScopeFilter(t *testing.T) {
	s, path := newTestStore(t)
	dir := filepath.Dir(path)
	s.Create("a", "", "work")
	s.Create("b", "", "Work")
	s.Create("c", "", "")

	for _, c := range []struct {
		scope string
		want  int
	}{
		{"", 3},
		{"work", 2}, // case-insensitive
		{dir, 1},    // the default
		{"other", 0},
	} {
		got, err := s.List(c.scope)
		if err != nil || got == nil || len(got) != c.want {
			t.Fatalf("List(%q) = %+v, %v; want %d tasks", c.scope, got, err, c.want)
		}
	}
	// The query text is matched against scope too.
	if got, _ := s.Search("work", ""); len(got) != 2 {
		t.Fatalf("want query to match scope, got %+v", got)
	}
	if got, _ := s.Search("", "work"); len(got) != 2 {
		t.Fatalf("want scope filter on search, got %+v", got)
	}
}

func TestLegacyFileWithoutScope(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.json")
	if err := os.WriteFile(path, []byte(`{"next_id":2,"tasks":[{"id":1,"title":"old"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s := openStore(t, path)
	dir := filepath.Dir(path)

	// Backfilled on read.
	got, err := s.Get(1)
	if err != nil || got.Scope != dir {
		t.Fatalf("want scope %q, got %+v, %v", dir, got, err)
	}
	// Persisted on the next write.
	if _, err := s.Update(1, new("renamed"), nil, nil); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	quoted, _ := json.Marshal(dir)
	if !bytes.Contains(b, []byte(`"scope": `+string(quoted))) {
		t.Fatalf("scope not persisted; file:\n%s", b)
	}
}

func TestListEmptyIsNonNil(t *testing.T) {
	s, _ := newTestStore(t)
	if got := list(t, s); got == nil || len(got) != 0 {
		t.Fatalf("want empty non-nil slice, got %#v", got)
	}
}

func TestPersistence(t *testing.T) {
	s, path := newTestStore(t)
	s.Create("one", "", "")
	s.Create("two", "desc", "")
	s.Delete(1)

	r := openStore(t, path)
	tasks := list(t, r)
	if len(tasks) != 1 || tasks[0].ID != 2 || tasks[0].Description != "desc" {
		t.Fatalf("unexpected reloaded tasks: %+v", tasks)
	}
	c, _ := r.Create("three", "", "")
	if c.ID != 3 {
		t.Fatalf("want ID 3 after reload, got %d", c.ID)
	}
}

func TestOpenStore(t *testing.T) {
	for _, c := range []struct {
		name, content string
		wantErr       string // substring of the error, or "" for success
		wantIDs       []int  // IDs List returns, in order
		wantNext      int    // ID the next Create gets
	}{
		{"empty file", "", "", []int{}, 1},
		{"corrupt", "{", "parse", nil, 0},
		{"next_id zero", `{"next_id":0,"tasks":[{"id":7,"title":"x"}]}`, "", []int{7}, 8},
		{"next_id zero, no tasks", `{"next_id":0,"tasks":[]}`, "", []int{}, 1},
		{"stale next_id", `{"next_id":1,"tasks":[{"id":7,"title":"x"}]}`, "", []int{7}, 8},
		{"duplicate id", `{"next_id":3,"tasks":[{"id":2,"title":"a"},{"id":2,"title":"b"}]}`, "duplicate task id 2", nil, 0},
		{"invalid id", `{"next_id":3,"tasks":[{"id":0,"title":"a"}]}`, "invalid task id 0", nil, 0},
		{"out of order", `{"next_id":4,"tasks":[{"id":3,"title":"c"},{"id":1,"title":"a"}]}`, "", []int{1, 3}, 4},
	} {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "tasks.json")
			if err := os.WriteFile(path, []byte(c.content), 0o644); err != nil {
				t.Fatal(err)
			}
			s, err := OpenStore(path)
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("want error containing %q, got %v", c.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()

			ids := []int{}
			for _, task := range list(t, s) {
				ids = append(ids, task.ID)
			}
			if !slices.Equal(ids, c.wantIDs) {
				t.Fatalf("want IDs %v, got %v", c.wantIDs, ids)
			}
			n, err := s.Create("next", "", "")
			if err != nil || n.ID != c.wantNext {
				t.Fatalf("want next ID %d, got %d (%v)", c.wantNext, n.ID, err)
			}
		})
	}
}

func TestSaveFailureLeavesFile(t *testing.T) {
	s, path := newTestStore(t)
	s.Create("keep", "", "")
	before, _ := os.ReadFile(path)

	forceSaveFailure(t, path) // platform-specific, see store_*_test.go

	if _, err := s.Create("lost", "", ""); err == nil {
		t.Fatal("want error from forced save failure")
	}
	if after, _ := os.ReadFile(path); !bytes.Equal(before, after) {
		t.Fatal("file changed after failed save")
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}
}

func TestSharedFile(t *testing.T) {
	a, path := newTestStore(t)
	b := openStore(t, path)

	if _, err := a.Create("from a", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Create("from b", "", ""); err != nil {
		t.Fatal(err)
	}
	for _, s := range []*Store{a, b} {
		got := list(t, s)
		if len(got) != 2 || got[0].ID != 1 || got[1].ID != 2 {
			t.Fatalf("want both tasks with IDs 1, 2, got %+v", got)
		}
	}

	// Concurrent creates through both stores must never reuse an ID.
	const n = 20
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			s := a
			if i%2 == 1 {
				s = b
			}
			if _, err := s.Create("concurrent", "", ""); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()

	got := list(t, a)
	if len(got) != n+2 {
		t.Fatalf("want %d tasks, got %d", n+2, len(got))
	}
	seen := map[int]bool{}
	for _, task := range got {
		if seen[task.ID] {
			t.Fatalf("duplicate ID %d", task.ID)
		}
		seen[task.ID] = true
	}
}
