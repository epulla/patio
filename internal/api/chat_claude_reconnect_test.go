package api

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestClaudeReconnectMCP(t *testing.T) {
	for _, tc := range []struct {
		name, subtype, message string
		wantErr                bool
	}{{name: "success", subtype: "success"}, {name: "error", subtype: "error", message: "server unavailable", wantErr: true}} {
		t.Run(tc.name, func(t *testing.T) {
			process := newPipeChatProcess()
			d := &claudeRuntime{process: process, processDone: make(chan struct{})}
			result := make(chan error, 1)
			go func() { result <- d.reconnectMCP("my-server") }()
			var request map[string]any
			select {
			case line := <-process.input:
				if err := json.Unmarshal([]byte(line), &request); err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("reconnect request not written")
			}
			if request["type"] != "control_request" || request["request"].(map[string]any)["serverName"] != "my-server" {
				t.Fatalf("request = %#v", request)
			}
			response, _ := json.Marshal(map[string]any{"type": "control_response", "response": map[string]string{"subtype": tc.subtype, "request_id": request["request_id"].(string), "error": tc.message}})
			if err := d.parseLine(response); err != nil {
				t.Fatal(err)
			}
			err := <-result
			if (err != nil) != tc.wantErr || (err != nil && !strings.Contains(err.Error(), tc.message)) {
				t.Fatalf("reconnect error = %v", err)
			}
			_ = process.finish()
		})
	}
}
