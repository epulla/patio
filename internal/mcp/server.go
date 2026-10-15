package mcp

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"patio/internal/api"
)

type boardArgs struct {
	BoardID string `json:"board_id" jsonschema:"Board UUID"`
}
type createBoardArgs struct {
	Name      string              `json:"name" jsonschema:"Board name"`
	ShortName string              `json:"short_name" jsonschema:"Short board code"`
	Statuses  []createBoardStatus `json:"statuses,omitempty" jsonschema:"Optional columns; type may be classic or temporary. Without type, hide_after_days makes column temporary; otherwise classic. Temporary columns default to 1 hide_after_days."`
}
type createBoardStatus struct {
	Name          string  `json:"name" jsonschema:"Column name"`
	Type          *string `json:"type,omitempty" jsonschema:"Optional column type: classic or temporary. If omitted, hide_after_days selects temporary; otherwise classic."`
	HideAfterDays *int    `json:"hide_after_days,omitempty" jsonschema:"Days before tickets auto-hide; when type is omitted, setting this makes column temporary. Temporary defaults to 1 day."`
}
type createBoardStatusBody struct {
	Name          string `json:"name"`
	Type          string `json:"type"`
	HideAfterDays *int   `json:"hide_after_days,omitempty"`
}
type createBoardBody struct {
	Name      string                  `json:"name"`
	ShortName string                  `json:"short_name"`
	Statuses  []createBoardStatusBody `json:"statuses,omitempty"`
}
type boardUpdateArgs struct {
	BoardID   string `json:"board_id"`
	Name      string `json:"name"`
	ShortName string `json:"short_name"`
	Template  string `json:"template,omitempty"`
}
type columnArgs struct {
	ID            string `json:"id,omitempty"`
	BoardID       string `json:"board_id,omitempty"`
	Name          string `json:"name"`
	Type          string `json:"type"`
	HideAfterDays *int   `json:"hide_after_days,omitempty"`
}
type reorderColumnsArgs struct {
	BoardID   string   `json:"board_id"`
	StatusIDs []string `json:"status_ids"`
}
type reorderBoardsArgs struct {
	BoardIDs []string `json:"board_ids"`
}
type columnIDArgs struct {
	ID string `json:"id"`
}
type ticketTypeArgs struct {
	ID      string `json:"id,omitempty"`
	BoardID string `json:"board_id,omitempty"`
	Name    string `json:"name"`
	Color   string `json:"color"`
}
type createTicketArgs struct {
	BoardID      string  `json:"board_id" jsonschema:"Board UUID"`
	Title        string  `json:"title" jsonschema:"Ticket title"`
	Description  *string `json:"description,omitempty" jsonschema:"Optional description"`
	StatusID     *string `json:"status_id,omitempty" jsonschema:"Optional status UUID"`
	TicketTypeID *string `json:"ticket_type_id,omitempty" jsonschema:"Optional ticket type UUID"`
	DueDate      *string `json:"due_date,omitempty" jsonschema:"Optional due date in YYYY-MM-DD format"`
}
type ticketIDArgs struct {
	ID string `json:"id" jsonschema:"Ticket UUID or code"`
}
type skillArgs struct {
	Name string `json:"name" jsonschema:"Skill name"`
}
type createSkillArgs struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Body        string `json:"body"`
}
type updateSkillArgs struct {
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
	Body        *string `json:"body,omitempty"`
	Enabled     *bool   `json:"enabled,omitempty"`
}
type updateTicketArgs struct {
	ID           string  `json:"id" jsonschema:"Ticket UUID"`
	Title        *string `json:"title,omitempty" jsonschema:"Optional new title"`
	Description  *string `json:"description,omitempty" jsonschema:"Optional new description"`
	DueDate      *string `json:"due_date,omitempty" jsonschema:"Optional new due date in YYYY-MM-DD format"`
	TicketTypeID *string `json:"ticket_type_id,omitempty" jsonschema:"Optional ticket type UUID"`
}
type moveTicketArgs struct {
	ID     string `json:"id" jsonschema:"Ticket UUID"`
	Status string `json:"status" jsonschema:"Status UUID or case-insensitive status name"`
}
type searchArgs struct {
	Query       string `json:"query,omitempty" jsonschema:"Optional title or code search text; omit to list all tickets"`
	BoardID     string `json:"board_id,omitempty" jsonschema:"Optional board UUID"`
	CreatedFrom string `json:"created_from,omitempty" jsonschema:"Optional RFC 3339 timestamp; created at or after"`
	CreatedTo   string `json:"created_to,omitempty" jsonschema:"Optional RFC 3339 timestamp; created before"`
	UpdatedFrom string `json:"updated_from,omitempty" jsonschema:"Optional RFC 3339 timestamp; updated at or after"`
	UpdatedTo   string `json:"updated_to,omitempty" jsonschema:"Optional RFC 3339 timestamp; updated before"`
	Sort        string `json:"sort,omitempty" jsonschema:"Optional sort: created (default), updated, title, code, board, status"`
	Dir         string `json:"dir,omitempty" jsonschema:"Optional asc or desc; default desc for created/updated, asc otherwise"`
}
type commentArgs struct {
	TicketID string `json:"ticket_id" jsonschema:"Ticket UUID"`
	Content  string `json:"content" jsonschema:"Comment text"`
}
type settingsDefaultsArgs struct {
	ConnectorID   json.RawMessage `json:"connector_id,omitempty" jsonschema:"Optional connector UUID or null to clear"`
	Folder        json.RawMessage `json:"folder,omitempty" jsonschema:"Optional folder path or null to clear"`
	Thinking      *bool           `json:"thinking,omitempty"`
	ShowThinking  *bool           `json:"show_thinking,omitempty"`
	MartinPersona *bool           `json:"martin_persona,omitempty"`
}
type listSkillsArgs struct {
	IncludeDisabled bool `json:"include_disabled,omitempty"`
}
type listCalendarArgs struct {
	From    string `json:"from" jsonschema:"Start date in YYYY-MM-DD format"`
	To      string `json:"to" jsonschema:"End date in YYYY-MM-DD format"`
	BoardID string `json:"board_id,omitempty" jsonschema:"Optional board UUID"`
}
type createCalendarItemArgs struct {
	Kind        string  `json:"kind" jsonschema:"event or reminder"`
	Title       string  `json:"title" jsonschema:"Calendar item title"`
	Description string  `json:"description,omitempty" jsonschema:"Optional description"`
	StartsAt    string  `json:"starts_at" jsonschema:"Start time; use YYYY-MM-DD for all-day items, timed items accept RFC3339 with optional offset (local time if omitted)"`
	EndsAt      *string `json:"ends_at,omitempty" jsonschema:"Optional end time; use YYYY-MM-DD for all-day items, timed items accept RFC3339 with optional offset (local time if omitted)"`
	AllDay      bool    `json:"all_day,omitempty" jsonschema:"True for all-day items using YYYY-MM-DD dates; omit or false for timed items, whose offset is optional (local time if omitted)"`
	BoardID     *string `json:"board_id,omitempty" jsonschema:"Optional board UUID"`
	TicketID    *string `json:"ticket_id,omitempty" jsonschema:"Optional linked ticket UUID"`
}
type updateCalendarItemArgs struct {
	ID          string  `json:"id" jsonschema:"Calendar item UUID"`
	Kind        *string `json:"kind,omitempty" jsonschema:"Optional event or reminder"`
	Title       *string `json:"title,omitempty" jsonschema:"Optional new title"`
	Description *string `json:"description,omitempty" jsonschema:"Optional new description"`
	StartsAt    *string `json:"starts_at,omitempty" jsonschema:"Optional new start time; use YYYY-MM-DD for all-day items, timed items accept RFC3339 with optional offset (local time if omitted)"`
	EndsAt      *string `json:"ends_at,omitempty" jsonschema:"Optional new end time; use YYYY-MM-DD for all-day items, timed items accept RFC3339 with optional offset (local time if omitted)"`
	AllDay      *bool   `json:"all_day,omitempty" jsonschema:"Optional all-day flag; dates use YYYY-MM-DD, timed values accept an optional offset (local time if omitted)"`
	BoardID     *string `json:"board_id,omitempty" jsonschema:"Optional board UUID"`
	TicketID    *string `json:"ticket_id,omitempty" jsonschema:"Optional linked ticket UUID"`
}
type calendarItemUpdateBody struct {
	Kind        *string `json:"kind,omitempty"`
	Title       *string `json:"title,omitempty"`
	Description *string `json:"description,omitempty"`
	StartsAt    *string `json:"starts_at,omitempty"`
	EndsAt      *string `json:"ends_at,omitempty"`
	AllDay      *bool   `json:"all_day,omitempty"`
	BoardID     *string `json:"board_id,omitempty"`
	TicketID    *string `json:"ticket_id,omitempty"`
}
type calendarItemIDArgs struct {
	ID string `json:"id" jsonschema:"Calendar item UUID"`
}
type scheduledTaskIDArgs struct {
	ID string `json:"id" jsonschema:"Scheduled task UUID"`
}
type scheduledTaskArgs struct {
	ID          string  `json:"id,omitempty" jsonschema:"Scheduled task UUID for updates"`
	Title       string  `json:"title" jsonschema:"Task title"`
	Prompt      string  `json:"prompt" jsonschema:"Prompt to run"`
	ConnectorID *string `json:"connector_id,omitempty" jsonschema:"Optional UUID from existing connectors"`
	Model       string  `json:"model"`
	Folder      *string `json:"folder,omitempty"`
	Thinking    bool    `json:"thinking"`
	AutoAllow   *bool   `json:"auto_allow,omitempty" jsonschema:"Defaults true when omitted"`
	Schedule    string  `json:"schedule,omitempty" jsonschema:"5-field cron in local time; exactly one of schedule or run_at"`
	RunAt       *string `json:"run_at,omitempty" jsonschema:"RFC3339 time for one-off; exactly one of schedule or run_at"`
	Enabled     *bool   `json:"enabled,omitempty" jsonschema:"Defaults true when omitted"`
}
type mcpServerIDArgs struct {
	ID string `json:"id" jsonschema:"MCP server UUID"`
}
type createMCPServerArgs struct {
	Name        string            `json:"name" jsonschema:"Server name"`
	Kind        string            `json:"kind" jsonschema:"stdio or http"`
	Command     string            `json:"command,omitempty" jsonschema:"stdio executable command"`
	Args        []string          `json:"args,omitempty" jsonschema:"stdio command arguments"`
	Env         map[string]string `json:"env,omitempty" jsonschema:"stdio environment variables; values are write-only and never returned"`
	URL         string            `json:"url,omitempty" jsonschema:"http server URL"`
	Auth        string            `json:"auth,omitempty" jsonschema:"http auth: none, bearer, or oauth"`
	BearerToken string            `json:"bearer_token,omitempty" jsonschema:"http bearer token; write-only and never returned"`
	Enabled     *bool             `json:"enabled,omitempty" jsonschema:"Whether server is enabled; defaults true"`
}
type updateMCPServerArgs struct {
	ID          string             `json:"id" jsonschema:"MCP server UUID"`
	Name        *string            `json:"name,omitempty" jsonschema:"Optional new server name"`
	Kind        *string            `json:"kind,omitempty" jsonschema:"Optional stdio or http kind"`
	Command     *string            `json:"command,omitempty" jsonschema:"Optional stdio executable command"`
	Args        *[]string          `json:"args,omitempty" jsonschema:"Optional stdio command arguments"`
	Env         *map[string]string `json:"env,omitempty" jsonschema:"Optional stdio environment variables; values are write-only and never returned"`
	URL         *string            `json:"url,omitempty" jsonschema:"Optional http server URL"`
	Auth        *string            `json:"auth,omitempty" jsonschema:"Optional http auth: none, bearer, or oauth"`
	BearerToken *string            `json:"bearer_token,omitempty" jsonschema:"Optional http bearer token; write-only and never returned"`
	Enabled     *bool              `json:"enabled,omitempty" jsonschema:"Whether server is enabled"`
}
type connectorIDArgs struct {
	ID string `json:"id" jsonschema:"Connector UUID"`
}
type createConnectorArgs struct {
	Kind             string  `json:"kind" jsonschema:"Connector kind: claude, codex, opencode, copilot, or local"`
	Name             string  `json:"name" jsonschema:"Connector name"`
	UseExistingLogin bool    `json:"use_existing_login,omitempty" jsonschema:"Use existing login instead of isolated configuration"`
	BaseURL          string  `json:"base_url,omitempty" jsonschema:"Base URL for local connectors"`
	Model            string  `json:"model,omitempty" jsonschema:"Default model"`
	Secret           *string `json:"secret,omitempty" jsonschema:"Write-only API key or PAT; never returned"`
}
type updateConnectorArgs struct {
	ID      string  `json:"id" jsonschema:"Connector UUID"`
	Name    *string `json:"name,omitempty" jsonschema:"Optional new name"`
	BaseURL *string `json:"base_url,omitempty" jsonschema:"Optional new base URL"`
	Model   *string `json:"model,omitempty" jsonschema:"Optional new default model"`
	Secret  *string `json:"secret,omitempty" jsonschema:"Write-only API key; never returned; empty clears secret"`
}

type approveArgs struct {
	ToolName  string          `json:"tool_name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
}

type ApprovalOptions struct {
	DB           *sql.DB
	ChatID       string
	Timeout      time.Duration
	PollInterval time.Duration
	InApp        bool
}

func NewServer(h http.Handler, version string, database *sql.DB, inApp bool) (*sdk.Server, error) {
	return newServer(h, version, database, nil, inApp)
}

func NewServerWithApproval(h http.Handler, version string, options ApprovalOptions) (*sdk.Server, error) {
	return newServer(h, version, options.DB, &options, options.InApp)
}

func newServer(h http.Handler, version string, database *sql.DB, approval *ApprovalOptions, inApp bool) (*sdk.Server, error) {
	var skills []api.Skill
	if database != nil {
		var err error
		skills, err = api.EnabledSkills(database)
		if err != nil {
			return nil, fmt.Errorf("load enabled skills: %w", err)
		}
	}
	instructions := "Your changes are labeled AI in the app. After finishing a multi-step task likely to recur (never one-offs), call create_skill and state the skill name and description in your reply."
	if !inApp {
		persona, err := api.MartinPersonaEnabled(database)
		if err != nil {
			return nil, fmt.Errorf("load Martin persona setting: %w", err)
		}
		instructions = api.ChatSystemPrompt(persona, "") + " " + instructions
	}
	if len(skills) > 0 {
		instructions += " When an enabled skill applies to a task, call load_skill and follow its instructions before replying."
	}
	s := sdk.NewServer(&sdk.Implementation{Name: "patio", Version: version}, &sdk.ServerOptions{Instructions: instructions})
	addGeneric(s, "list_boards", "List all boards", h, func(struct{}) request { return request{http.MethodGet, "/api/boards", nil} })
	addGeneric(s, "reorder_boards", "Set sidebar board order", h, func(a reorderBoardsArgs) request {
		return request{http.MethodPut, "/api/board-order", map[string]any{"board_ids": a.BoardIDs}}
	})
	addGeneric(s, "list_mcp_servers", "List MCP servers; env values and bearer tokens are write-only and never returned", h, func(struct{}) request { return request{http.MethodGet, "/api/mcp-servers", nil} })
	addGeneric(s, "create_mcp_server", "Register MCP server. stdio uses command, args, and env; http uses url, auth, and bearer_token. env and bearer_token are write-only", h, func(a createMCPServerArgs) request {
		return request{http.MethodPost, "/api/mcp-servers", a}
	})
	addGeneric(s, "update_mcp_server", "Update provided MCP server fields. Disable with enabled=false. stdio uses command, args, and env; http uses url, auth, and bearer_token. env and bearer_token are write-only", h, func(a updateMCPServerArgs) request {
		body := struct {
			Name        *string            `json:"name,omitempty"`
			Kind        *string            `json:"kind,omitempty"`
			Command     *string            `json:"command,omitempty"`
			Args        *[]string          `json:"args,omitempty"`
			Env         *map[string]string `json:"env,omitempty"`
			URL         *string            `json:"url,omitempty"`
			Auth        *string            `json:"auth,omitempty"`
			BearerToken *string            `json:"bearer_token,omitempty"`
			Enabled     *bool              `json:"enabled,omitempty"`
		}{a.Name, a.Kind, a.Command, a.Args, a.Env, a.URL, a.Auth, a.BearerToken, a.Enabled}
		return request{http.MethodPut, "/api/mcp-servers/" + url.PathEscape(a.ID), body}
	})
	addGeneric(s, "check_mcp_server", "Check MCP server connectivity and list its tools", h, func(a mcpServerIDArgs) request {
		return request{http.MethodGet, "/api/mcp-servers/" + url.PathEscape(a.ID) + "/check", nil}
	})
	addGeneric(s, "list_connectors", "List AI connectors; secrets are write-only", h, func(struct{}) request { return request{http.MethodGet, "/api/connectors", nil} })
	addGeneric(s, "create_connector", "Create an AI connector; secret is a write-only API key", h, func(a createConnectorArgs) request { return request{http.MethodPost, "/api/connectors", a} })
	addGeneric(s, "update_connector", "Update provided connector fields; secret is write-only", h, func(a updateConnectorArgs) request {
		return request{http.MethodPut, "/api/connectors/" + url.PathEscape(a.ID), struct {
			Name    *string `json:"name,omitempty"`
			BaseURL *string `json:"base_url,omitempty"`
			Model   *string `json:"model,omitempty"`
			Secret  *string `json:"secret,omitempty"`
		}{a.Name, a.BaseURL, a.Model, a.Secret}}
	})
	addGeneric(s, "list_connector_models", "List models available from a connector", h, func(a connectorIDArgs) request {
		return request{http.MethodGet, "/api/connectors/" + url.PathEscape(a.ID) + "/models", nil}
	})
	addGeneric(s, "get_board", "Get board data including statuses, types, and tickets", h, func(a boardArgs) request {
		return request{http.MethodGet, "/api/boards/" + url.PathEscape(a.BoardID) + "/data", nil}
	})
	addGeneric(s, "create_board", "Create a board with a name and a required short_name code like MP. Column type may be classic or temporary; without type, hide_after_days selects temporary, otherwise classic. Temporary columns default to 1 hide_after_days.", h, func(a createBoardArgs) request {
		body := createBoardBody{Name: a.Name, ShortName: a.ShortName}
		if a.Statuses != nil {
			body.Statuses = make([]createBoardStatusBody, len(a.Statuses))
			for i, status := range a.Statuses {
				typeName := "classic"
				if status.Type != nil {
					typeName = *status.Type
				} else if status.HideAfterDays != nil {
					typeName = "temporary"
				}
				body.Statuses[i] = createBoardStatusBody{status.Name, typeName, status.HideAfterDays}
			}
		}
		return request{http.MethodPost, "/api/boards", body}
	})
	addGeneric(s, "update_board", "Update board name, short name, and template", h, func(a boardUpdateArgs) request {
		return request{http.MethodPut, "/api/boards/" + url.PathEscape(a.BoardID), map[string]any{"name": a.Name, "short_name": a.ShortName, "template": a.Template}}
	})
	addGeneric(s, "create_column", "Create a board column; type is classic or temporary", h, func(a columnArgs) request {
		return request{http.MethodPost, "/api/boards/" + url.PathEscape(a.BoardID) + "/statuses", map[string]any{"name": a.Name, "type": a.Type, "hide_after_days": a.HideAfterDays}}
	})
	addGeneric(s, "update_column", "Update column name, type, and optional hide_after_days", h, func(a columnArgs) request {
		return request{http.MethodPut, "/api/statuses/" + url.PathEscape(a.ID), map[string]any{"name": a.Name, "type": a.Type, "hide_after_days": a.HideAfterDays}}
	})
	addGeneric(s, "reorder_columns", "Set board column order", h, func(a reorderColumnsArgs) request {
		return request{http.MethodPut, "/api/boards/" + url.PathEscape(a.BoardID) + "/status-order", map[string]any{"status_ids": a.StatusIDs}}
	})
	addGenericHandler(s, "delete_column", "Delete an empty column; only columns without tickets can be deleted", func(a columnIDArgs) (*sdk.CallToolResult, error) {
		_, err := doJSON(h, request{http.MethodDelete, "/api/statuses/" + url.PathEscape(a.ID), nil})
		if err != nil {
			return errorResult(err), nil
		}
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "deleted"}}}, nil
	})
	addGeneric(s, "create_ticket_type", "Create a ticket type for a board", h, func(a ticketTypeArgs) request {
		return request{http.MethodPost, "/api/boards/" + url.PathEscape(a.BoardID) + "/ticket-types", map[string]string{"name": a.Name, "color": a.Color}}
	})
	addGeneric(s, "update_ticket_type", "Update ticket type name and color", h, func(a ticketTypeArgs) request {
		return request{http.MethodPut, "/api/ticket-types/" + url.PathEscape(a.ID), map[string]string{"name": a.Name, "color": a.Color}}
	})
	addGeneric(s, "create_ticket", "Create a ticket", h, func(a createTicketArgs) request {
		return request{http.MethodPost, "/api/boards/" + url.PathEscape(a.BoardID) + "/tickets", a}
	})
	addGeneric(s, "get_ticket", "Get ticket by UUID or code", h, func(a ticketIDArgs) request {
		return request{http.MethodGet, "/api/tickets/" + url.PathEscape(a.ID), nil}
	})
	addGeneric(s, "update_ticket", "Update provided ticket fields", h, func(a updateTicketArgs) request {
		return request{http.MethodPut, "/api/tickets/" + url.PathEscape(a.ID), a}
	})
	addGenericHandler(s, "move_ticket", "Move ticket to status UUID or name", moveHandler(h))
	addGeneric(s, "search_tickets", "Search tickets by title or code, filter by board and created/updated time, and sort; newest first, at most 200", h, func(a searchArgs) request {
		q := url.Values{}
		for k, v := range map[string]string{"q": a.Query, "board_id": a.BoardID, "created_from": a.CreatedFrom, "created_to": a.CreatedTo, "updated_from": a.UpdatedFrom, "updated_to": a.UpdatedTo, "sort": a.Sort, "dir": a.Dir} {
			if v != "" {
				q.Set(k, v)
			}
		}
		return request{http.MethodGet, "/api/tickets/search?" + q.Encode(), nil}
	})
	addGeneric(s, "add_comment", "Add a ticket comment", h, func(a commentArgs) request {
		return request{http.MethodPost, "/api/tickets/" + url.PathEscape(a.TicketID) + "/comments", map[string]string{"content": a.Content}}
	})
	addGeneric(s, "list_comments", "List ticket comments", h, func(a ticketIDArgs) request {
		return request{http.MethodGet, "/api/tickets/" + url.PathEscape(a.ID) + "/comments", nil}
	})
	addGeneric(s, "get_ticket_history", "Get ticket history", h, func(a ticketIDArgs) request {
		return request{http.MethodGet, "/api/tickets/" + url.PathEscape(a.ID) + "/history", nil}
	})
	addGeneric(s, "list_users", "List users", h, func(struct{}) request { return request{http.MethodGet, "/api/users", nil} })
	addGeneric(s, "get_settings_defaults", "Get chat settings defaults", h, func(struct{}) request { return request{http.MethodGet, "/api/settings/defaults", nil} })
	addGeneric(s, "update_settings_defaults", "Update settings defaults; auto_allow cannot be changed", h, func(a settingsDefaultsArgs) request {
		body := map[string]any{}
		for key, value := range map[string]json.RawMessage{"connector_id": a.ConnectorID, "folder": a.Folder} {
			if len(value) > 0 {
				body[key] = value
			}
		}
		for key, value := range map[string]*bool{"thinking": a.Thinking, "show_thinking": a.ShowThinking, "martin_persona": a.MartinPersona} {
			if value != nil {
				body[key] = *value
			}
		}
		return request{http.MethodPut, "/api/settings/defaults", body}
	})
	addGeneric(s, "list_calendar", "List calendar items and tickets due in a date range", h, func(a listCalendarArgs) request {
		q := url.Values{"from": {a.From}, "to": {a.To}}
		if a.BoardID != "" {
			q.Set("board_id", a.BoardID)
		}
		return request{http.MethodGet, "/api/calendar?" + q.Encode(), nil}
	})
	addGeneric(s, "list_scheduled_tasks", "List scheduled tasks", h, func(struct{}) request { return request{http.MethodGet, "/api/scheduled-tasks", nil} })
	addGeneric(s, "get_scheduled_task", "Get scheduled task including recent runs", h, func(a scheduledTaskIDArgs) request {
		return request{http.MethodGet, "/api/scheduled-tasks/" + url.PathEscape(a.ID), nil}
	})
	description := "Schedule is a 5-field cron in local time or run_at RFC3339 for one-off (exactly one); auto_allow defaults true; connector_id must come from existing connectors."
	addGeneric(s, "create_scheduled_task", "Create scheduled task. "+description, h, func(a scheduledTaskArgs) request { return request{http.MethodPost, "/api/scheduled-tasks", a} })
	addGeneric(s, "update_scheduled_task", "Update scheduled task; all fields are replaced. "+description, h, func(a scheduledTaskArgs) request {
		return request{http.MethodPut, "/api/scheduled-tasks/" + url.PathEscape(a.ID), a}
	})
	addGenericHandler(s, "delete_scheduled_task", "Delete scheduled task", func(a scheduledTaskIDArgs) (*sdk.CallToolResult, error) {
		_, err := doJSON(h, request{http.MethodDelete, "/api/scheduled-tasks/" + url.PathEscape(a.ID), nil})
		if err != nil {
			return errorResult(err), nil
		}
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "deleted"}}}, nil
	})
	addGeneric(s, "create_calendar_item", "Create a calendar event or reminder", h, func(a createCalendarItemArgs) request {
		return request{http.MethodPost, "/api/calendar-items", a}
	})
	addGeneric(s, "update_calendar_item", "Update provided calendar item fields", h, func(a updateCalendarItemArgs) request {
		body := calendarItemUpdateBody{a.Kind, a.Title, a.Description, a.StartsAt, a.EndsAt, a.AllDay, a.BoardID, a.TicketID}
		return request{http.MethodPut, "/api/calendar-items/" + url.PathEscape(a.ID), body}
	})
	addGenericHandler(s, "delete_calendar_item", "Delete a calendar item", func(a calendarItemIDArgs) (*sdk.CallToolResult, error) {
		_, err := doJSON(h, request{http.MethodDelete, "/api/calendar-items/" + url.PathEscape(a.ID), nil})
		if err != nil {
			return errorResult(err), nil
		}
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "deleted"}}}, nil
	})
	addSkillTools(s, h, database, skills)
	addQueryTools(s, database)
	if approval != nil {
		if approval.Timeout == 0 {
			approval.Timeout = 10 * time.Minute
		}
		if approval.PollInterval == 0 {
			approval.PollInterval = 300 * time.Millisecond
		}
		sdk.AddTool[approveArgs, any](s, &sdk.Tool{
			Name: "approve", Description: "Request user approval for a Claude tool call",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{
				"tool_name": map[string]any{"type": "string"}, "input": map[string]any{"type": "object"},
				"tool_use_id": map[string]any{"type": "string"},
			}, "required": []string{"tool_name", "input"}, "additionalProperties": true},
		}, func(ctx context.Context, _ *sdk.CallToolRequest, in approveArgs) (*sdk.CallToolResult, any, error) {
			return approvalHandler(ctx, approval, in)
		})
	}
	return s, nil
}

func addSkillTools(s *sdk.Server, h http.Handler, database *sql.DB, skills []api.Skill) {
	description := "No skills are enabled."
	if len(skills) > 0 {
		var available strings.Builder
		available.WriteString("Load a skill's instructions before doing a task it covers. Available skills:")
		for _, skill := range skills {
			fmt.Fprintf(&available, "\n- %s: %s", skill.Name, skill.Description)
		}
		description = available.String()
	}
	sdk.AddTool[listSkillsArgs, any](s, &sdk.Tool{Name: "list_skills", Description: "List enabled skills; optionally include disabled skills"}, func(_ context.Context, _ *sdk.CallToolRequest, in listSkillsArgs) (*sdk.CallToolResult, any, error) {
		if database == nil {
			return errorResult(fmt.Errorf("skills database unavailable")), nil, nil
		}
		var enabled []api.Skill
		var err error
		if in.IncludeDisabled {
			data, e := doJSON(h, request{http.MethodGet, "/api/skills", nil})
			err = e
			if err == nil {
				err = json.Unmarshal(data, &enabled)
			}
		} else {
			enabled, err = api.EnabledSkills(database)
		}
		if err != nil {
			return errorResult(err), nil, nil
		}
		result := make([]struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Enabled     *bool  `json:"enabled,omitempty"`
		}, 0, len(enabled))
		for _, skill := range enabled {
			item := struct {
				Name        string `json:"name"`
				Description string `json:"description"`
				Enabled     *bool  `json:"enabled,omitempty"`
			}{Name: skill.Name, Description: skill.Description}
			if in.IncludeDisabled {
				item.Enabled = &skill.Enabled
			}
			result = append(result, item)
		}
		data, err := json.Marshal(result)
		if err != nil {
			return errorResult(err), nil, nil
		}
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: string(data)}}}, nil, nil
	})
	sdk.AddTool[skillArgs, any](s, &sdk.Tool{Name: "load_skill", Description: description}, func(_ context.Context, _ *sdk.CallToolRequest, in skillArgs) (*sdk.CallToolResult, any, error) {
		if database == nil {
			return errorResult(fmt.Errorf("skills database unavailable")), nil, nil
		}
		skill, err := api.SkillByName(database, in.Name)
		if errors.Is(err, sql.ErrNoRows) || err == nil && !skill.Enabled {
			return errorResult(fmt.Errorf("skill not found: %s", in.Name)), nil, nil
		}
		if err != nil {
			return errorResult(err), nil, nil
		}
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "# " + skill.Name + "\n\n" + skill.Body}}}, nil, nil
	})
	sdk.AddTool[createSkillArgs, any](s, &sdk.Tool{Name: "create_skill", Description: "Create a skill"}, func(_ context.Context, _ *sdk.CallToolRequest, in createSkillArgs) (*sdk.CallToolResult, any, error) {
		data, err := doJSON(h, request{http.MethodPost, "/api/skills", in})
		if err != nil {
			if strings.Contains(err.Error(), "skill name already exists") {
				err = fmt.Errorf("%w; use update_skill", err)
			}
			return errorResult(err), nil, nil
		}
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: string(data)}}}, nil, nil
	})
	sdk.AddTool[updateSkillArgs, any](s, &sdk.Tool{Name: "update_skill", Description: "Update a skill by name"}, func(_ context.Context, _ *sdk.CallToolRequest, in updateSkillArgs) (*sdk.CallToolResult, any, error) {
		if database == nil {
			return errorResult(fmt.Errorf("skills database unavailable")), nil, nil
		}
		skill, err := api.SkillByName(database, in.Name)
		if errors.Is(err, sql.ErrNoRows) {
			return errorResult(fmt.Errorf("skill not found: %s", in.Name)), nil, nil
		}
		if err != nil {
			return errorResult(err), nil, nil
		}
		body := struct {
			Description *string `json:"description,omitempty"`
			Body        *string `json:"body,omitempty"`
			Enabled     *bool   `json:"enabled,omitempty"`
		}{in.Description, in.Body, in.Enabled}
		result, err := do(h, request{http.MethodPut, "/api/skills/" + url.PathEscape(skill.ID), body})
		return result, nil, err
	})
	sdk.AddTool[skillArgs, any](s, &sdk.Tool{Name: "delete_skill", Description: "Delete a skill by name"}, func(_ context.Context, _ *sdk.CallToolRequest, in skillArgs) (*sdk.CallToolResult, any, error) {
		if database == nil {
			return errorResult(fmt.Errorf("skills database unavailable")), nil, nil
		}
		skill, err := api.SkillByName(database, in.Name)
		if errors.Is(err, sql.ErrNoRows) {
			return errorResult(fmt.Errorf("skill not found: %s", in.Name)), nil, nil
		}
		if err != nil {
			return errorResult(err), nil, nil
		}
		_, err = doJSON(h, request{http.MethodDelete, "/api/skills/" + url.PathEscape(skill.ID), nil})
		if err != nil {
			return errorResult(err), nil, nil
		}
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "deleted"}}}, nil, nil
	})
}

func approvalHandler(ctx context.Context, options *ApprovalOptions, in approveArgs) (*sdk.CallToolResult, any, error) {
	if in.ToolName == "" {
		return nil, nil, fmt.Errorf("tool_name required")
	}
	var input map[string]json.RawMessage
	if err := json.Unmarshal(in.Input, &input); err != nil {
		return nil, nil, fmt.Errorf("input must be an object: %w", err)
	}
	if input == nil {
		return nil, nil, fmt.Errorf("input must be an object")
	}
	toolName := strings.TrimPrefix(in.ToolName, "mcp__patio__")
	if toolName == "list_skills" || toolName == "load_skill" {
		return approvalAllowResult(in.Input)
	}
	id, auto, err := api.CreateChatApproval(options.DB, options.ChatID, in.ToolName, in.Input)
	if err != nil {
		return nil, nil, err
	}
	if auto {
		return approvalAllowResult(in.Input)
	}
	timer := time.NewTimer(options.Timeout)
	defer timer.Stop()
	ticker := time.NewTicker(options.PollInterval)
	defer ticker.Stop()
	for {
		status, err := api.ChatApprovalStatus(options.DB, options.ChatID, id)
		if err != nil {
			return nil, nil, err
		}
		switch status {
		case "allowed":
			return approvalAllowResult(in.Input)
		case "denied":
			return approvalResult([]byte(`{"behavior":"deny","message":"Denied by user"}`)), nil, nil
		case "expired":
			return approvalResult([]byte(`{"behavior":"deny","message":"Approval timed out"}`)), nil, nil
		}
		select {
		case <-ctx.Done():
			status, err := api.ExpireChatApproval(options.DB, options.ChatID, id)
			if err != nil {
				return nil, nil, err
			}
			if status == "allowed" {
				return approvalAllowResult(in.Input)
			}
			return approvalResult([]byte(`{"behavior":"deny","message":"Denied by user"}`)), nil, nil
		case <-timer.C:
			status, err := api.ExpireChatApproval(options.DB, options.ChatID, id)
			if err != nil {
				return nil, nil, err
			}
			if status == "allowed" {
				return approvalAllowResult(in.Input)
			}
			if status == "denied" {
				return approvalResult([]byte(`{"behavior":"deny","message":"Denied by user"}`)), nil, nil
			}
			return approvalResult([]byte(`{"behavior":"deny","message":"Approval timed out"}`)), nil, nil
		case <-ticker.C:
		}
	}
}

func approvalAllowResult(input json.RawMessage) (*sdk.CallToolResult, any, error) {
	data, err := json.Marshal(struct {
		Behavior     string          `json:"behavior"`
		UpdatedInput json.RawMessage `json:"updatedInput"`
	}{"allow", input})
	return approvalResult(data), nil, err
}

func approvalResult(data []byte) *sdk.CallToolResult {
	return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: string(data)}}}
}

func Check(ctx context.Context, t sdk.Transport) ([]*sdk.Tool, time.Duration, error) {
	start := time.Now()
	client := sdk.NewClient(&sdk.Implementation{Name: "patio-check", Version: "1"}, nil)
	session, err := client.Connect(ctx, t, nil)
	if err != nil {
		return nil, time.Since(start), fmt.Errorf("connect: %w", err)
	}
	result, err := session.ListTools(ctx, nil)
	elapsed := time.Since(start)
	closeErr := session.Close()
	if err != nil {
		return nil, elapsed, fmt.Errorf("list tools: %w", err)
	}
	if closeErr != nil {
		return nil, elapsed, fmt.Errorf("close session: %w", closeErr)
	}
	return result.Tools, elapsed, nil
}

type request struct {
	method, path string
	body         any
}

func addGeneric[T any](s *sdk.Server, name, description string, h http.Handler, makeRequest func(T) request) {
	sdk.AddTool[T, any](s, &sdk.Tool{Name: name, Description: description}, func(ctx context.Context, req *sdk.CallToolRequest, in T) (*sdk.CallToolResult, any, error) {
		result, err := do(h, makeRequest(in))
		return result, nil, err
	})
}

func addGenericHandler[T any](s *sdk.Server, name, description string, handler func(T) (*sdk.CallToolResult, error)) {
	sdk.AddTool[T, any](s, &sdk.Tool{Name: name, Description: description}, func(ctx context.Context, req *sdk.CallToolRequest, in T) (*sdk.CallToolResult, any, error) {
		result, err := handler(in)
		return result, nil, err
	})
}

func moveHandler(h http.Handler) func(moveTicketArgs) (*sdk.CallToolResult, error) {
	return func(in moveTicketArgs) (*sdk.CallToolResult, error) {
		ticket, err := doJSON(h, request{http.MethodGet, "/api/tickets/" + url.PathEscape(in.ID), nil})
		if err != nil {
			return errorResult(err), nil
		}
		var t struct {
			BoardID string `json:"board_id"`
		}
		if err := json.Unmarshal(ticket, &t); err != nil {
			return nil, err
		}
		data, err := doJSON(h, request{http.MethodGet, "/api/boards/" + url.PathEscape(t.BoardID) + "/data", nil})
		if err != nil {
			return errorResult(err), nil
		}
		var board struct {
			Statuses []struct{ ID, Name string } `json:"statuses"`
		}
		if err := json.Unmarshal(data, &board); err != nil {
			return nil, err
		}
		status := in.Status
		statusLabel := in.Status
		for _, candidate := range board.Statuses {
			if strings.EqualFold(candidate.Name, status) {
				status = candidate.ID
				statusLabel = candidate.Name
				break
			}
		}
		return do(h, request{method: http.MethodPut, path: "/api/tickets/" + url.PathEscape(in.ID) + "/move?mcp_status_label=" + url.QueryEscape(statusLabel), body: struct {
			StatusID string `json:"status_id"`
			Index    int    `json:"index"`
		}{status, math.MaxInt32}}) // API clamps to column length: append at end
	}
}

func do(h http.Handler, r request) (*sdk.CallToolResult, error) {
	body, err := doJSON(h, r)
	if err != nil {
		return errorResult(err), nil
	}
	return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: string(body)}}}, nil
}

func doJSON(h http.Handler, r request) ([]byte, error) {
	var body io.Reader
	if r.body != nil {
		data, err := json.Marshal(r.body)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(data)
	}
	req := httptest.NewRequest(r.method, "http://mcp.local"+r.path, body)
	if r.body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	data := res.Body.Bytes()
	if res.Code < 200 || res.Code >= 300 {
		return nil, apiError(data, res.Code)
	}
	if r.method == http.MethodPost || r.method == http.MethodPut || r.method == http.MethodDelete {
		if notifier, ok := h.(interface {
			Notify(string, string, string, string, bool) error
		}); ok {
			if title, link, write := mcpChange(r, data); write {
				if err := notifier.Notify("change", title, "", link, false); err != nil {
					log.Printf("store MCP change notification: %v", err)
				}
			}
		}
	}
	return data, nil
}

func mcpChange(r request, response []byte) (string, string, bool) {
	parts := strings.Split(strings.SplitN(r.path, "?", 2)[0], "/")
	resource := ""
	for i := 1; i < len(parts); i++ {
		if parts[i] == "api" {
			if i+1 < len(parts) {
				resource = parts[i+1]
			}
			break
		}
	}
	if resource == "" {
		return "", "", false
	}
	verb := map[string]string{http.MethodPost: "created", http.MethodPut: "updated", http.MethodDelete: "deleted"}[r.method]
	target := ""
	var payload map[string]any
	if r.body != nil {
		b, err := json.Marshal(r.body)
		if err != nil || json.Unmarshal(b, &payload) != nil {
			payload = nil
		}
	}
	var result map[string]any
	if json.Unmarshal(response, &result) != nil {
		result = nil
	}
	title := ""
	if strings.Contains(r.path, "/comments") {
		for i, part := range parts {
			if part == "tickets" && i+1 < len(parts) {
				return "Martin commented on " + parts[i+1], "/tickets/" + parts[i+1], true
			}
		}
	}
	if strings.Contains(r.path, "/tickets") && resource == "boards" && r.method == http.MethodPost && result != nil {
		id, _ := result["id"].(string)
		code, _ := result["code"].(string)
		if id != "" {
			if code == "" {
				code = id
			}
			return "Martin created " + code, "/tickets/" + id, true
		}
	}
	switch resource {
	case "tickets":
		id := ""
		for i, p := range parts {
			if p == "tickets" && i+1 < len(parts) {
				id = parts[i+1]
				break
			}
		}
		if id == "" && result != nil {
			id, _ = result["id"].(string)
		}
		code := id
		if result != nil {
			if v, ok := result["code"].(string); ok && v != "" {
				code = v
			}
		}
		if code != "" {
			target = code
		}
		if strings.Contains(r.path, "/move") {
			title = "Martin moved " + code
			if parsed, err := url.Parse(r.path); err == nil && parsed.Query().Get("mcp_status_label") != "" {
				title += " to " + parsed.Query().Get("mcp_status_label")
			} else if result != nil {
				if status, ok := result["status_name"].(string); ok && status != "" {
					title += " to " + status
				}
			}
		} else if r.method == http.MethodPost {
			title = "Martin created " + code
		} else {
			title = "Martin updated " + code
		}
		if id != "" {
			return title, "/tickets/" + id, true
		}
	case "comments":
		id := ""
		for i, p := range parts {
			if p == "tickets" && i+1 < len(parts) {
				id = parts[i+1]
			}
		}
		return "Martin commented on " + id, "/tickets/" + id, true
	case "boards", "statuses", "ticket-types", "skills", "scheduled-tasks", "connectors", "mcp-servers", "calendar-items", "board-order":
		if resource == "boards" && len(parts) > 3 && parts[3] != "" && r.method == http.MethodPut {
			return "Martin updated board " + parts[3], "/boards/" + parts[3], true
		}
		if result != nil {
			if id, ok := result["id"].(string); ok {
				target = "/boards/" + id
				if resource == "scheduled-tasks" {
					target = "/ai/scheduled/" + id
				}
				if resource == "calendar-items" {
					target = "/calendar"
				}
			}
		}
		return "Martin " + verb + " " + strings.ReplaceAll(strings.TrimSuffix(resource, "s"), "-", " "), target, true
	default:
		return "", "", false
	}
	return "Martin " + verb + " " + resource, "", true
}

func apiError(data []byte, code int) error {
	var out struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(data, &out) == nil && out.Error != "" {
		return fmt.Errorf("%s", out.Error)
	}
	return fmt.Errorf("API request failed (%d): %s", code, strings.TrimSpace(string(data)))
}

func errorResult(err error) *sdk.CallToolResult {
	return &sdk.CallToolResult{IsError: true, Content: []sdk.Content{&sdk.TextContent{Text: err.Error()}}}
}
