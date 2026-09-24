package native

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

const codexAppServerInitTimeout = 15 * time.Second

type codexRPC interface {
	Call(context.Context, string, any, any) error
	Close() error
}

type codexRPCError struct {
	Code    int
	Message string
}

func (e *codexRPCError) Error() string {
	return fmt.Sprintf("Codex app-server error %d: %s", e.Code, e.Message)
}

type codexRPCResponse struct {
	ID     json.RawMessage `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *codexRPCError  `json:"error"`
}

type codexRPCClient struct {
	binary string
	home   string

	mu      sync.Mutex
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	encoder *json.Encoder
	pending map[int64]chan codexRPCResponse
	nextID  int64
	ready   chan struct{}
	closed  bool
}

func newCodexRPCClient(binary, home string) *codexRPCClient {
	return &codexRPCClient{binary: binary, home: home}
}

func (c *codexRPCClient) Call(ctx context.Context, method string, params, result any) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return fmt.Errorf("Codex app-server client is closed")
	}
	if c.cmd == nil {
		if err := c.startLocked(); err != nil {
			c.mu.Unlock()
			return err
		}
	}
	ready := c.ready
	c.mu.Unlock()

	if ready != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ready:
		}
	}
	return c.send(ctx, method, params, result)
}

func (c *codexRPCClient) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	stdin := c.stdin
	cmd := c.cmd
	c.stdin = nil
	c.encoder = nil
	c.cmd = nil
	c.failPendingLocked(fmt.Errorf("Codex app-server client closed"))
	if c.ready != nil {
		close(c.ready)
		c.ready = nil
	}
	c.mu.Unlock()

	stopCodexProcess(cmd, stdin)
	return nil
}

func (c *codexRPCClient) startLocked() error {
	if c.closed {
		return fmt.Errorf("Codex app-server client is closed")
	}
	cmd := exec.Command(c.binary, "app-server", "--stdio")
	cmd.Env = replaceEnv(os.Environ(), "CODEX_HOME", c.home)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("open Codex app-server stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("open Codex app-server stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("open Codex app-server stderr: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start Codex app-server %q: %w", c.binary, err)
	}

	c.cmd = cmd
	c.stdin = stdin
	c.encoder = json.NewEncoder(stdin)
	c.pending = make(map[int64]chan codexRPCResponse)
	c.ready = make(chan struct{})

	go func() {
		_, _ = io.Copy(io.Discard, stderr)
	}()
	go c.readLoop(cmd, json.NewDecoder(stdout))
	go c.initialize(cmd)
	return nil
}

func (c *codexRPCClient) initialize(cmd *exec.Cmd) {
	ctx, cancel := context.WithTimeout(context.Background(), codexAppServerInitTimeout)
	defer cancel()

	var result json.RawMessage
	err := c.send(ctx, "initialize", map[string]any{
		"clientInfo": map[string]any{
			"name":    "closeview",
			"version": "1",
		},
		"capabilities": map[string]any{
			"experimentalApi": false,
		},
	}, &result)
	if err == nil {
		err = c.notify("initialized", map[string]any{})
	}
	if err != nil {
		c.fail(cmd, err)
		return
	}

	c.mu.Lock()
	if c.cmd == cmd && c.ready != nil {
		close(c.ready)
		c.ready = nil
	}
	c.mu.Unlock()
}

func (c *codexRPCClient) send(ctx context.Context, method string, params, result any) error {
	c.mu.Lock()
	if c.cmd == nil || c.encoder == nil {
		c.mu.Unlock()
		return fmt.Errorf("Codex app-server is not running")
	}
	c.nextID++
	id := c.nextID
	response := make(chan codexRPCResponse, 1)
	c.pending[id] = response
	if err := c.encoder.Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}); err != nil {
		delete(c.pending, id)
		cmd := c.cmd
		stdin := c.stdin
		c.failLocked(cmd, fmt.Errorf("write Codex app-server request: %w", err))
		c.mu.Unlock()
		stopCodexProcess(cmd, stdin)
		return err
	}
	c.mu.Unlock()

	select {
	case <-ctx.Done():
		c.removePending(id)
		return ctx.Err()
	case reply := <-response:
		if reply.Error != nil {
			return reply.Error
		}
		if result == nil || len(reply.Result) == 0 || string(reply.Result) == "null" {
			return nil
		}
		if err := json.Unmarshal(reply.Result, result); err != nil {
			return fmt.Errorf("decode Codex app-server %s response: %w", method, err)
		}
		return nil
	}
}

func (c *codexRPCClient) notify(method string, params any) error {
	c.mu.Lock()
	if c.cmd == nil || c.encoder == nil {
		c.mu.Unlock()
		return fmt.Errorf("Codex app-server is not running")
	}
	if err := c.encoder.Encode(map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	}); err != nil {
		cmd := c.cmd
		stdin := c.stdin
		c.failLocked(cmd, fmt.Errorf("write Codex app-server notification: %w", err))
		c.mu.Unlock()
		stopCodexProcess(cmd, stdin)
		return err
	}
	c.mu.Unlock()
	return nil
}

func (c *codexRPCClient) readLoop(cmd *exec.Cmd, decoder *json.Decoder) {
	for {
		var reply codexRPCResponse
		if err := decoder.Decode(&reply); err != nil {
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			_ = cmd.Wait()
			c.fail(cmd, fmt.Errorf("read Codex app-server response: %w", err))
			return
		}
		if len(reply.ID) == 0 {
			continue
		}
		id, err := strconv.ParseInt(strings.TrimSpace(string(reply.ID)), 10, 64)
		if err != nil {
			continue
		}
		c.mu.Lock()
		response := c.pending[id]
		delete(c.pending, id)
		c.mu.Unlock()
		if response != nil {
			response <- reply
		}
	}
}

func (c *codexRPCClient) removePending(id int64) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

func (c *codexRPCClient) fail(cmd *exec.Cmd, err error) {
	c.mu.Lock()
	if cmd == nil || c.cmd != cmd {
		c.mu.Unlock()
		return
	}
	stdin := c.stdin
	c.failLocked(cmd, err)
	c.mu.Unlock()
	stopCodexProcess(cmd, stdin)
}

func (c *codexRPCClient) failLocked(cmd *exec.Cmd, err error) {
	if cmd == nil || c.cmd != cmd {
		return
	}
	c.cmd = nil
	c.stdin = nil
	c.encoder = nil
	c.failPendingLocked(err)
	if c.ready != nil {
		close(c.ready)
		c.ready = nil
	}
}

func (c *codexRPCClient) failPendingLocked(err error) {
	for id, response := range c.pending {
		response <- codexRPCResponse{Error: &codexRPCError{Code: -1, Message: err.Error()}}
		delete(c.pending, id)
	}
}

func stopCodexProcess(cmd *exec.Cmd, stdin io.WriteCloser) {
	if stdin != nil {
		_ = stdin.Close()
	}
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

func replaceEnv(environment []string, key, value string) []string {
	prefix := key + "="
	updated := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		if !strings.HasPrefix(entry, prefix) {
			updated = append(updated, entry)
		}
	}
	return append(updated, prefix+value)
}
