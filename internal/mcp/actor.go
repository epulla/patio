package mcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"sync"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"patio/internal/api"
)

type actorContextKey struct{}

func mcpActor(ctx context.Context) string {
	actor, _ := ctx.Value(actorContextKey{}).(string)
	return actor
}

func changeActorMiddleware(db *sql.DB) sdk.Middleware {
	var mu sync.RWMutex
	added := map[string]bool{}
	var listed sync.Once
	return func(next sdk.MethodHandler) sdk.MethodHandler {
		return func(ctx context.Context, method string, req sdk.Request) (sdk.Result, error) {
			if method == "tools/list" {
				result, err := next(ctx, method, req)
				list, ok := result.(*sdk.ListToolsResult)
				if err != nil || !ok {
					return result, err
				}
				copyResult := *list
				copyResult.Tools = append([]*sdk.Tool(nil), list.Tools...)
				for i, tool := range list.Tools {
					if readOnlyTool(tool.Name) || hasChatID(tool.InputSchema) {
						continue
					}
					var schema map[string]any
					data, err := json.Marshal(tool.InputSchema)
					if err != nil || json.Unmarshal(data, &schema) != nil {
						continue
					}
					props, _ := schema["properties"].(map[string]any)
					if props == nil {
						props = map[string]any{}
					}
					props["chat_id"] = map[string]any{"type": "string", "description": "Optional: this chat's ID from your instructions; credits your bot in Patio's change log"}
					schema["properties"] = props
					withChat := *tool
					withChat.InputSchema = schema
					copyResult.Tools[i] = &withChat
					mu.Lock()
					added[tool.Name] = true
					mu.Unlock()
				}
				return &copyResult, nil
			}
			if method == "tools/call" {
				if serverReq, ok := req.(*sdk.ServerRequest[*sdk.CallToolParamsRaw]); ok {
					listed.Do(func() {
						rememberAdded(next, ctx, &sdk.ServerRequest[*sdk.ListToolsParams]{Session: serverReq.Session, Params: &sdk.ListToolsParams{}}, added, &mu)
					})
				}
				if call, ok := req.GetParams().(*sdk.CallToolParamsRaw); ok {
					var args map[string]json.RawMessage
					if json.Unmarshal(call.Arguments, &args) == nil && args != nil {
						var chatID string
						_ = json.Unmarshal(args["chat_id"], &chatID)
						if strings.TrimSpace(chatID) != "" {
							if name, err := api.ChatBotName(db, chatID); err == nil && name != "" {
								ctx = context.WithValue(ctx, actorContextKey{}, name)
							}
						}
						mu.RLock()
						synthetic := added[call.Name]
						mu.RUnlock()
						if synthetic {
							delete(args, "chat_id")
							call.Arguments, _ = json.Marshal(args)
							if serverReq, ok := req.(*sdk.ServerRequest[*sdk.CallToolParamsRaw]); ok {
								serverReq.Params = call
							}
						}
					}
				}
			}
			return next(ctx, method, req)
		}
	}
}

func rememberAdded(next sdk.MethodHandler, ctx context.Context, req sdk.Request, added map[string]bool, mu *sync.RWMutex) {
	result, err := next(ctx, "tools/list", req)
	list, ok := result.(*sdk.ListToolsResult)
	if err != nil || !ok {
		return
	}
	for _, tool := range list.Tools {
		if readOnlyTool(tool.Name) {
			continue
		}
		if !hasChatID(tool.InputSchema) {
			mu.Lock()
			added[tool.Name] = true
			mu.Unlock()
		}
	}
}

func hasChatID(schema any) bool {
	data, err := json.Marshal(schema)
	if err != nil {
		return false
	}
	var decoded struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	return json.Unmarshal(data, &decoded) == nil && decoded.Properties["chat_id"] != nil
}

func readOnlyTool(name string) bool {
	for _, prefix := range []string{"list_", "get_", "search_", "load_", "query_", "check_", "test_"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}
