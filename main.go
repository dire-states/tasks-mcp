// Command tasks-mcp is an MCP server that manages tasks stored in a JSON file.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// version is reported to clients in the server's Implementation info.
const version = "0.1.0"

func main() {
	defaultFile := "tasks.json"
	if env := os.Getenv("TASKS_FILE"); env != "" {
		defaultFile = env
	}
	file := flag.String("file", defaultFile, "path to the tasks JSON file (env TASKS_FILE)")
	httpAddr := flag.String("http", "", "serve Streamable HTTP on this address (e.g. 127.0.0.1:8080) instead of stdio; there is no authentication")
	flag.Parse()

	// stdout carries the stdio protocol; keep logs on stderr.
	log.SetOutput(os.Stderr)

	store, err := OpenStore(*file)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer store.Close()

	server := newServer(store)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if *httpAddr == "" {
		log.Printf("tasks-mcp %s serving stdio (file %s)", version, *file)
		if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil && !errors.Is(err, context.Canceled) {
			log.Fatal(err)
		}
		return
	}

	// Bind first so "serving" is only logged once the address is ours.
	ln, err := net.Listen("tcp", *httpAddr)
	if err != nil {
		log.Fatal(err)
	}
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}

	// On a signal, stop accepting and drain in-flight requests. Serve returns
	// as soon as the listener closes, so main waits on done before exiting.
	done := make(chan struct{})
	go func() {
		defer close(done)
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("shutdown: %v", err)
		}
	}()
	log.Printf("tasks-mcp %s serving Streamable HTTP on %s (file %s)", version, ln.Addr(), *file)
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
	<-done
}

// newServer builds the MCP server with all task tools registered on store.
func newServer(store *Store) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "tasks-mcp", Version: version}, nil)
	registerTools(server, store)
	return server
}
