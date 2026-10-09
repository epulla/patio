package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSkillValidation(t *testing.T) {
	valid := skillConfig{Name: "valid-name-2", Description: "a description", Body: "body"}
	tests := []struct {
		name   string
		config skillConfig
		valid  bool
	}{
		{"valid", valid, true},
		{"empty name", func() skillConfig { c := valid; c.Name = ""; return c }(), false},
		{"uppercase name", func() skillConfig { c := valid; c.Name = "Bad"; return c }(), false},
		{"underscore name", func() skillConfig { c := valid; c.Name = "bad_name"; return c }(), false},
		{"leading hyphen", func() skillConfig { c := valid; c.Name = "-bad"; return c }(), false},
		{"trailing hyphen", func() skillConfig { c := valid; c.Name = "bad-"; return c }(), false},
		{"repeated hyphen", func() skillConfig { c := valid; c.Name = "bad--name"; return c }(), false},
		{"64 character name", func() skillConfig { c := valid; c.Name = strings.Repeat("a", 64); return c }(), true},
		{"65 character name", func() skillConfig { c := valid; c.Name = strings.Repeat("a", 65); return c }(), false},
		{"empty trimmed description", func() skillConfig { c := valid; c.Description = " \t\n"; return c }(), false},
		{"1024 character description", func() skillConfig { c := valid; c.Description = strings.Repeat("é", 1024); return c }(), true},
		{"1025 character description", func() skillConfig { c := valid; c.Description = strings.Repeat("é", 1025); return c }(), false},
		{"64 KB body", func() skillConfig { c := valid; c.Body = strings.Repeat("x", maxSkillBody); return c }(), true},
		{"over 64 KB body", func() skillConfig { c := valid; c.Body = strings.Repeat("x", maxSkillBody+1); return c }(), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateSkill(tc.config)
			if (err == nil) != tc.valid {
				t.Fatalf("validateSkill() error = %v, valid = %v", err, tc.valid)
			}
		})
	}
}

func TestParseSkill(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		want    skillConfig
		wantErr string
	}{
		{"quoted values", "---\nname: 'code-review'\ndescription: \"Review: carefully\"\n---\n\nBody\n", skillConfig{Name: "code-review", Description: "Review: carefully", Body: "Body\n", Enabled: true}, ""},
		{"folded description", "---\nname: helper\ndescription: >\n  First line\n  second line\n---\n\nText", skillConfig{Name: "helper", Description: "First line second line", Body: "Text", Enabled: true}, ""},
		{"literal description", "---\nname: helper\ndescription: |\n  First line\n  second line\n---\n\nText", skillConfig{Name: "helper", Description: "First line\nsecond line", Body: "Text", Enabled: true}, ""},
		{"CRLF", "---\r\nname: crlf\r\ndescription: works\r\n---\r\n\r\nBody", skillConfig{Name: "crlf", Description: "works", Body: "Body", Enabled: true}, ""},
		{"BOM", "\xef\xbb\xbf---\nname: bom\ndescription: works\n---\n\nBody", skillConfig{Name: "bom", Description: "works", Body: "Body", Enabled: true}, ""},
		{"missing name", "---\ndescription: works\n---\nbody", skillConfig{}, "missing name"},
		{"missing description", "---\nname: helper\n---\nbody", skillConfig{}, "missing description"},
		{"no front matter", "name: helper\ndescription: works", skillConfig{}, "start with front matter"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseSkill(tc.text)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("parseSkill() error = %v, want containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("parseSkill() = %#v, %v; want %#v", got, err, tc.want)
			}
		})
	}
}

func TestSkillCRUDAndImportExport(t *testing.T) {
	database, handler, _ := chatAPI(t)
	create := map[string]any{"name": "zebra", "description": "  Handles: punctuation  ", "body": "# Body\n\nText"}
	code, raw := requestRaw(t, handler, http.MethodPost, "/api/skills", create)
	if code != http.StatusCreated {
		t.Fatalf("create status/body = %d %s", code, raw)
	}
	var created Skill
	if err := json.Unmarshal([]byte(raw), &created); err != nil {
		t.Fatal(err)
	}
	if created.Name != "zebra" || created.Description != "  Handles: punctuation  " || created.Body != create["body"] || !created.Enabled || created.ID == "" {
		t.Fatalf("created skill = %#v", created)
	}
	if code := requestJSON(t, handler, http.MethodPost, "/api/skills", create, nil); code != http.StatusConflict {
		t.Fatalf("duplicate create status = %d", code)
	}
	var second Skill
	if code := requestJSON(t, handler, http.MethodPost, "/api/skills", map[string]any{"name": "alpha", "description": "first"}, &second); code != http.StatusCreated {
		t.Fatalf("second create status = %d", code)
	}
	var listed []Skill
	if code := requestJSON(t, handler, http.MethodGet, "/api/skills", nil, &listed); code != http.StatusOK || len(listed) != 2 || listed[0].Name != "alpha" || listed[1].Name != "zebra" {
		t.Fatalf("list status/skills = %d, %#v", code, listed)
	}
	if code := requestJSON(t, handler, http.MethodGet, "/api/skills/"+created.ID, nil, &listed[0]); code != http.StatusOK || listed[0].ID != created.ID {
		t.Fatalf("get status/skill = %d, %#v", code, listed[0])
	}
	if code := requestJSON(t, handler, http.MethodPut, "/api/skills/"+created.ID, map[string]any{"enabled": false, "body": "updated"}, &created); code != http.StatusOK || created.Enabled || created.Body != "updated" || created.Description != "  Handles: punctuation  " {
		t.Fatalf("partial update status/skill = %d, %#v", code, created)
	}
	if code := requestJSON(t, handler, http.MethodPut, "/api/skills/"+created.ID, map[string]any{"name": "alpha"}, nil); code != http.StatusConflict {
		t.Fatalf("duplicate rename status = %d", code)
	}
	if code, body := requestRaw(t, handler, http.MethodGet, "/api/skills/"+created.ID+"/export", nil); code != http.StatusOK || !strings.Contains(body, `description: "  Handles: punctuation  "`) {
		t.Fatalf("export status/body = %d %s", code, body)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/skills/"+created.ID+"/export", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Header().Get("Content-Type") != "text/markdown; charset=utf-8" || response.Header().Get("Content-Disposition") != `attachment; filename="SKILL.md"` {
		t.Fatalf("export headers = %#v", response.Header())
	}
	exported := response.Body.String()
	if code := requestJSON(t, handler, http.MethodPost, "/api/skills/import", map[string]string{"text": exported}, nil); code != http.StatusConflict {
		t.Fatalf("duplicate import status = %d", code)
	}
	importText := strings.Replace(exported, "name: zebra", "name: zebra-copy", 1)
	var imported Skill
	if code := requestJSON(t, handler, http.MethodPost, "/api/skills/import", map[string]string{"text": importText}, &imported); code != http.StatusCreated {
		t.Fatalf("import status = %d", code)
	}
	if imported.Name != "zebra-copy" || imported.Description != created.Description || imported.Body != created.Body {
		t.Fatalf("round trip skill = %#v; source %#v", imported, created)
	}
	enabled, err := EnabledSkills(database)
	if err != nil || len(enabled) != 2 || enabled[0].Name != "alpha" || enabled[1].Name != "zebra-copy" {
		t.Fatalf("enabled skills = %#v, %v", enabled, err)
	}
	byName, err := SkillByName(database, "zebra-copy")
	if err != nil || byName.ID != imported.ID {
		t.Fatalf("skill by name = %#v, %v", byName, err)
	}
	if code := requestJSON(t, handler, http.MethodDelete, "/api/skills/"+created.ID, nil, nil); code != http.StatusNoContent {
		t.Fatalf("delete status = %d", code)
	}
	if code := requestJSON(t, handler, http.MethodDelete, "/api/skills/missing", nil, nil); code != http.StatusNotFound {
		t.Fatalf("delete missing status = %d", code)
	}
}

func TestSkillInvalidRequests(t *testing.T) {
	_, handler, _ := chatAPI(t)
	for _, tc := range []struct {
		name string
		body any
	}{
		{"invalid name", map[string]any{"name": "Bad Name", "description": "description"}},
		{"missing description", map[string]any{"name": "missing-description"}},
		{"invalid import", map[string]any{"text": "---\nname: missing-description\n---\nbody"}},
		{"missing front matter", map[string]any{"text": "not a skill"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := "/api/skills"
			if strings.Contains(tc.name, "import") || strings.Contains(tc.name, "front matter") {
				path += "/import"
			}
			if code := requestJSON(t, handler, http.MethodPost, path, tc.body, nil); code != http.StatusBadRequest {
				t.Fatalf("invalid request status = %d", code)
			}
		})
	}
}
