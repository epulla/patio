package api

import (
	"sort"
)

func claudeMCPServers(ticket map[string]any, external []mcpServerSpec) map[string]any {
	servers := map[string]any{"my-tickets": ticket}
	for _, spec := range external {
		if spec.Kind == "stdio" {
			servers[spec.Name] = map[string]any{"command": spec.Command, "args": spec.Args, "env": nonNilMap(spec.Env)}
			continue
		}
		servers[spec.Name] = map[string]any{"type": "http", "url": spec.URL, "headers": mcpSpecHeaders(spec)}
	}
	return servers
}

func codexMCPServers(ticket map[string]any, external []mcpServerSpec) map[string]any {
	servers := codexExternalMCPServers(external)
	servers["my-tickets"] = ticket
	return servers
}

func copilotMCPServers(ticket map[string]any, external []mcpServerSpec) []any {
	servers := []any{map[string]any{"name": "my-tickets", "command": ticket["command"], "args": ticket["args"], "env": []any{}}}
	for _, spec := range external {
		if spec.Kind == "stdio" {
			servers = append(servers, map[string]any{
				"name": spec.Name, "command": spec.Command, "args": spec.Args, "env": copilotEnv(spec.Env),
			})
			continue
		}
		servers = append(servers, map[string]any{
			"type": "http", "name": spec.Name, "url": spec.URL, "headers": copilotHeaders(mcpSpecHeaders(spec)),
		})
	}
	return servers
}

func opencodeMCPServers(exe string, ticketArgs []string, external []mcpServerSpec) map[string]any {
	servers := map[string]any{"my-tickets": map[string]any{
		"type": "local", "command": append([]string{exe}, ticketArgs...), "enabled": true,
	}}
	for _, spec := range external {
		if spec.Kind == "stdio" {
			servers[spec.Name] = map[string]any{
				"type": "local", "command": append([]string{spec.Command}, spec.Args...), "environment": nonNilMap(spec.Env),
			}
			continue
		}
		servers[spec.Name] = map[string]any{"type": "remote", "url": spec.URL, "headers": mcpSpecHeaders(spec)}
	}
	return servers
}

func nonNilMap(values map[string]string) map[string]string {
	if values == nil {
		return map[string]string{}
	}
	return values
}

func copilotEnv(env map[string]string) []any {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	values := make([]any, 0, len(keys))
	for _, key := range keys {
		values = append(values, map[string]string{"name": key, "value": env[key]})
	}
	return values
}

func copilotHeaders(headers map[string]string) []any {
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	values := make([]any, 0, len(keys))
	for _, key := range keys {
		values = append(values, map[string]string{"name": key, "value": headers[key]})
	}
	return values
}
