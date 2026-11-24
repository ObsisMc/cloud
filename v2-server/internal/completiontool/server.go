package completiontool

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"
)

// protocolVersion is the MCP version the endpoint answers with, desktop's.
const protocolVersion = "2025-03-26"

const (
	maxRequestBytes = 64 << 10
	// connectionDeadline bounds one request, so a slow client holds nothing.
	connectionDeadline = 10 * time.Second
	pathPrefix         = "/mcp/"
)

// Server is the running loopback endpoint.
type Server struct {
	http *http.Server
	done chan struct{}
}

// Serve binds a loopback port and serves r's tool until Close. Agents run on this host in this
// process's network namespace (Node is a local child process), which is what makes a plain loopback
// URL reachable and allowed. Attempts must be opened after Serve.
func Serve(r *Registry) (*Server, error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	r.url = "http://" + listener.Addr().String() + pathPrefix
	r.mu.Unlock()
	s := &Server{done: make(chan struct{}), http: &http.Server{
		Handler: r, ReadTimeout: connectionDeadline, WriteTimeout: connectionDeadline, IdleTimeout: connectionDeadline,
		ErrorLog: log.New(io.Discard, "", 0), // a request line carries a credential
	}}
	go func() {
		defer close(s.done)
		_ = s.http.Serve(listener)
	}()
	return s, nil
}

// Close stops serving.
func (s *Server) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if s.http.Shutdown(ctx) != nil {
		_ = s.http.Close()
	}
	<-s.done
}

type rpcMessage struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params struct {
		Name      string                     `json:"name"`
		Arguments map[string]json.RawMessage `json:"arguments"`
	} `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// ServeHTTP answers the Streamable HTTP MCP endpoint of one credential. It offers no server-sent
// stream, so GET is refused and every answer is plain JSON.
func (r *Registry) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	credential, ok := strings.CutPrefix(req.URL.Path, pathPrefix)
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	nodeRun, session, ok := r.authorize(credential)
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	switch req.Method {
	case http.MethodDelete:
		w.WriteHeader(http.StatusOK)
		return
	case http.MethodPost:
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, req.Body, maxRequestBytes))
	if err != nil {
		var large *http.MaxBytesError
		if errors.As(err, &large) {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
		} else {
			w.WriteHeader(http.StatusBadRequest)
		}
		return
	}
	var message rpcMessage
	if json.Unmarshal(body, &message) != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if len(message.ID) == 0 || string(message.ID) == "null" {
		w.WriteHeader(http.StatusAccepted) // a notification has no answer
		return
	}
	w.Header().Set("Content-Type", "application/json")
	response := rpcResponse{JSONRPC: "2.0", ID: message.ID}
	response.Result, response.Error = r.answer(req.Context(), credential, nodeRun, session, message)
	_ = json.NewEncoder(w).Encode(response)
}

func (r *Registry) answer(ctx context.Context, credential, nodeRun, session string, message rpcMessage) (any, *rpcError) {
	switch message.Method {
	case "initialize":
		return map[string]any{"protocolVersion": protocolVersion, "capabilities": map[string]any{"tools": map[string]any{}},
			"serverInfo": map[string]string{"name": ServerName, "version": "0.1.0"}}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": []any{map[string]any{
			"name":        ToolName,
			"description": "Report that this workflow step is complete. Pass the attempt token from the step boundary.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"attemptToken": map[string]string{"type": "string"}},
				"required": []string{"attemptToken"}, "additionalProperties": false},
		}}}, nil
	case "tools/call":
		if message.Params.Name != ToolName {
			return nil, &rpcError{-32602, "unknown tool"}
		}
		var token string
		if json.Unmarshal(message.Params.Arguments["attemptToken"], &token) != nil || token == "" {
			return nil, &rpcError{-32602, "attemptToken is required"}
		}
		text, refused := r.call(ctx, credential, nodeRun, session, token)
		return map[string]any{"content": []any{map[string]string{"type": "text", "text": text}}, "isError": refused}, nil
	}
	return nil, &rpcError{-32601, "method not found"}
}

// call answers one tool call. Stored state is read first, so a node that left its turn refuses even
// when this process has not caught up; accept then decides under the registry's lock, so a turn
// settling meanwhile either takes the signal or refuses it as stale.
func (r *Registry) call(ctx context.Context, credential, nodeRun, session, token string) (text string, refused bool) {
	live, err := r.live(ctx, nodeRun, session)
	if err != nil {
		return string(RefusedUnavailable), true
	}
	if !live {
		// Name a token's own reason when there is one; nothing is recorded either way.
		r.mu.Lock()
		why, known := r.revoked[token]
		r.mu.Unlock()
		if !known {
			why = RefusedNotAwaiting
		}
		return string(why), true
	}
	signal, why := r.accept(credential, token)
	if why != "" {
		return string(why), true
	}
	return "accepted:" + signal.ID, false
}
