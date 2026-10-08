package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type boardArgs struct {
	BoardID string `json:"board_id" jsonschema:"Board UUID"`
}
type createBoardArgs struct {
	Name      string   `json:"name" jsonschema:"Board name"`
	ShortName string   `json:"short_name" jsonschema:"Short board code"`
	Statuses  []string `json:"statuses,omitempty" jsonschema:"Optional status names"`
}
type createTicketArgs struct {
	BoardID      string  `json:"board_id" jsonschema:"Board UUID"`
	Title        string  `json:"title" jsonschema:"Ticket title"`
	Description  *string `json:"description,omitempty" jsonschema:"Optional description"`
	StatusID     *string `json:"status_id,omitempty" jsonschema:"Optional status UUID"`
	TicketTypeID *string `json:"ticket_type_id,omitempty" jsonschema:"Optional ticket type UUID"`
	DueDate      *string `json:"due_date,omitempty" jsonschema:"Optional due date"`
}
type ticketIDArgs struct {
	ID string `json:"id" jsonschema:"Ticket UUID or code"`
}
type updateTicketArgs struct {
	ID           string  `json:"id" jsonschema:"Ticket UUID"`
	Title        *string `json:"title,omitempty" jsonschema:"Optional new title"`
	Description  *string `json:"description,omitempty" jsonschema:"Optional new description"`
	DueDate      *string `json:"due_date,omitempty" jsonschema:"Optional new due date"`
	TicketTypeID *string `json:"ticket_type_id,omitempty" jsonschema:"Optional ticket type UUID"`
}
type moveTicketArgs struct {
	ID     string `json:"id" jsonschema:"Ticket UUID"`
	Status string `json:"status" jsonschema:"Status UUID or case-insensitive status name"`
}
type searchArgs struct {
	Query   string `json:"query" jsonschema:"Title or code search text"`
	BoardID string `json:"board_id,omitempty" jsonschema:"Optional board UUID"`
}
type commentArgs struct {
	TicketID string `json:"ticket_id" jsonschema:"Ticket UUID"`
	Content  string `json:"content" jsonschema:"Comment text"`
}

func NewServer(h http.Handler) *sdk.Server {
	s := sdk.NewServer(&sdk.Implementation{Name: "my-tickets", Version: "1"}, nil)
	addGeneric(s, "list_boards", "List all boards", h, func(struct{}) request { return request{http.MethodGet, "/api/boards", nil} })
	addGeneric(s, "get_board", "Get board data including statuses, types, and tickets", h, func(a boardArgs) request {
		return request{http.MethodGet, "/api/boards/" + url.PathEscape(a.BoardID) + "/data", nil}
	})
	addGeneric(s, "create_board", "Create a board", h, func(a createBoardArgs) request { return request{http.MethodPost, "/api/boards", a} })
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
	addGeneric(s, "search_tickets", "Search tickets by title or code", h, func(a searchArgs) request {
		q := url.Values{"q": {a.Query}}
		if a.BoardID != "" {
			q.Set("board_id", a.BoardID)
		}
		return request{http.MethodGet, "/api/tickets/search?" + q.Encode(), nil}
	})
	addGeneric(s, "add_comment", "Add a ticket comment", h, func(a commentArgs) request {
		return request{http.MethodPost, "/api/tickets/" + url.PathEscape(a.TicketID) + "/comments", map[string]string{"content": a.Content}}
	})
	return s
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
		for _, candidate := range board.Statuses {
			if strings.EqualFold(candidate.Name, status) {
				status = candidate.ID
				break
			}
		}
		return do(h, request{http.MethodPut, "/api/tickets/" + url.PathEscape(in.ID) + "/move", struct {
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
	return data, nil
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
