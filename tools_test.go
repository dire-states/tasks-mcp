package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// connect starts an in-memory server on a fresh store and returns a client
// session connected to it.
func connect(t *testing.T) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "tasks.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	st, ct := mcp.NewInMemoryTransports()
	if _, err := newServer(store).Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

// call invokes a tool and decodes its structured output into out.
func call(t *testing.T, cs *mcp.ClientSession, name string, args any, out any) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if out != nil && !res.IsError {
		b, err := json.Marshal(res.StructuredContent)
		if err != nil {
			t.Fatalf("%s: encode: %v", name, err)
		}
		if err := json.Unmarshal(b, out); err != nil {
			t.Fatalf("%s: decode: %v", name, err)
		}
	}
	return res
}

// errorText returns the text of an error result.
func errorText(res *mcp.CallToolResult) string {
	if len(res.Content) == 0 {
		return ""
	}
	if tc, ok := res.Content[0].(*mcp.TextContent); ok {
		return tc.Text
	}
	return ""
}

func TestTools(t *testing.T) {
	cs := connect(t)

	tools, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 6 {
		t.Fatalf("want 6 tools, got %d", len(tools.Tools))
	}
	for _, tool := range tools.Tools {
		readOnly := tool.Annotations != nil && tool.Annotations.ReadOnlyHint
		want := tool.Name == "list_tasks" || tool.Name == "get_task" || tool.Name == "search_tasks"
		if readOnly != want {
			t.Errorf("%s: readOnlyHint = %v, want %v", tool.Name, readOnly, want)
		}
	}

	// An empty store lists as {"tasks": []}, not null.
	res := call(t, cs, "list_tasks", map[string]any{}, nil)
	if sc, _ := res.StructuredContent.(map[string]any); sc == nil {
		t.Fatalf("list on empty store: want object, got %v", res.StructuredContent)
	} else if _, ok := sc["tasks"].([]any); !ok {
		t.Fatalf("list on empty store: want tasks array, got %v", sc["tasks"])
	}

	var task Task
	call(t, cs, "create_task", map[string]any{"title": "Write docs", "description": "README"}, &task)
	if task.ID != 1 || task.Title != "Write docs" {
		t.Fatalf("create: %+v", task)
	}
	call(t, cs, "create_task", map[string]any{"title": "Ship it"}, nil)

	var list taskList
	call(t, cs, "list_tasks", map[string]any{}, &list)
	if len(list.Tasks) != 2 {
		t.Fatalf("list: %+v", list)
	}

	call(t, cs, "update_task", map[string]any{"id": 1, "title": "Write README"}, &task)
	if task.Title != "Write README" || task.Description != "README" {
		t.Fatalf("update: %+v", task)
	}
	modified := task.ModifiedAt
	call(t, cs, "update_task", map[string]any{"id": 1}, &task)
	if task.Title != "Write README" || !task.ModifiedAt.Equal(modified) {
		t.Fatalf("update with no fields changed the task: %+v", task)
	}

	call(t, cs, "get_task", map[string]any{"id": 1}, &task)
	if task.Title != "Write README" {
		t.Fatalf("get: %+v", task)
	}

	call(t, cs, "search_tasks", map[string]any{"query": "ship"}, &list)
	if len(list.Tasks) != 1 || list.Tasks[0].ID != 2 {
		t.Fatalf("search: %+v", list)
	}
	call(t, cs, "search_tasks", map[string]any{"query": ""}, &list)
	if len(list.Tasks) != 2 {
		t.Fatalf("search with empty query: %+v", list)
	}

	var del deleteResult
	call(t, cs, "delete_task", map[string]any{"id": 2}, &del)
	if del.Deleted != 2 {
		t.Fatalf("delete: %+v", del)
	}

	for _, c := range []struct {
		name string
		args map[string]any
		want string // substring of the error text
	}{
		{"get_task", map[string]any{"id": 2}, "task not found"},
		{"delete_task", map[string]any{"id": 2}, "task not found"},
		{"update_task", map[string]any{"id": 99, "title": "x"}, "task not found"},
		{"create_task", map[string]any{"title": ""}, "title must not be empty"},
	} {
		res := call(t, cs, c.name, c.args, nil)
		if !res.IsError {
			t.Errorf("%s %v: want tool error", c.name, c.args)
		} else if text := errorText(res); !strings.Contains(text, c.want) {
			t.Errorf("%s %v: want error containing %q, got %q", c.name, c.args, c.want, text)
		}
	}
}
