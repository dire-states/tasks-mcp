package main

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// createTaskInput is the argument set for create_task.
type createTaskInput struct {
	Title       string `json:"title" jsonschema:"short task title (required, non-blank)"`
	Description string `json:"description,omitempty" jsonschema:"optional longer description"`
}

// idInput is the argument set for tools that address a single task by ID.
type idInput struct {
	ID int `json:"id" jsonschema:"task ID"`
}

// updateTaskInput is the argument set for update_task; a nil field is kept
// as it is.
type updateTaskInput struct {
	ID          int     `json:"id" jsonschema:"task ID"`
	Title       *string `json:"title,omitempty" jsonschema:"new title; omit to keep the current one"`
	Description *string `json:"description,omitempty" jsonschema:"new description; omit to keep the current one"`
}

// searchTasksInput is the argument set for search_tasks.
type searchTasksInput struct {
	Query string `json:"query" jsonschema:"text to look for in titles and descriptions (case-insensitive); empty matches all"`
}

// taskList wraps a slice because structured tool output must be a JSON
// object, not a bare array.
type taskList struct {
	Tasks []Task `json:"tasks"`
}

// deleteResult is the structured output of delete_task.
type deleteResult struct {
	Deleted int `json:"deleted" jsonschema:"ID of the deleted task"`
}

// Annotation presets. DestructiveHint and OpenWorldHint default to true in
// the SDK, so they are set explicitly where they do not apply.
var (
	readOnly   = &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: new(false)}
	additive   = &mcp.ToolAnnotations{DestructiveHint: new(false), OpenWorldHint: new(false)}
	idempotent = &mcp.ToolAnnotations{IdempotentHint: true, OpenWorldHint: new(false)}
)

// registerTools adds the task tools to server, backed by store.
func registerTools(server *mcp.Server, store *Store) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_task",
		Title:       "Create task",
		Description: "Create a new task with a title and optional description.",
		Annotations: additive,
	}, func(_ context.Context, _ *mcp.CallToolRequest, in createTaskInput) (*mcp.CallToolResult, Task, error) {
		t, err := store.Create(in.Title, in.Description)
		return nil, t, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_tasks",
		Title:       "List tasks",
		Description: "List all tasks, ordered by ID.",
		Annotations: readOnly,
	}, func(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, taskList, error) {
		tasks, err := store.List()
		return nil, taskList{Tasks: tasks}, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_task",
		Title:       "Get task",
		Description: "Get a single task by ID.",
		Annotations: readOnly,
	}, func(_ context.Context, _ *mcp.CallToolRequest, in idInput) (*mcp.CallToolResult, Task, error) {
		t, err := store.Get(in.ID)
		return nil, t, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "update_task",
		Title:       "Update task",
		Description: "Update a task's title and/or description. Only the fields provided are changed.",
		Annotations: idempotent,
	}, func(_ context.Context, _ *mcp.CallToolRequest, in updateTaskInput) (*mcp.CallToolResult, Task, error) {
		t, err := store.Update(in.ID, in.Title, in.Description)
		return nil, t, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "delete_task",
		Title:       "Delete task",
		Description: "Delete a task by ID.",
		Annotations: idempotent,
	}, func(_ context.Context, _ *mcp.CallToolRequest, in idInput) (*mcp.CallToolResult, deleteResult, error) {
		if err := store.Delete(in.ID); err != nil {
			return nil, deleteResult{}, err
		}
		return nil, deleteResult{Deleted: in.ID}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_tasks",
		Title:       "Search tasks",
		Description: "Find tasks whose title or description contains the query (case-insensitive). An empty query returns all tasks.",
		Annotations: readOnly,
	}, func(_ context.Context, _ *mcp.CallToolRequest, in searchTasksInput) (*mcp.CallToolResult, taskList, error) {
		tasks, err := store.Search(in.Query)
		return nil, taskList{Tasks: tasks}, err
	})
}
