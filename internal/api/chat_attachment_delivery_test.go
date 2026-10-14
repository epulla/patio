package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	connectorcli "patio/internal/connector"
)

func deliveryAttachments(t *testing.T) (string, []ChatAttachment, []byte) {
	t.Helper()
	workdir := t.TempDir()
	dir := filepath.Join(workdir, "attachments")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	imageData := redSquarePNG(t)
	if err := os.WriteFile(filepath.Join(dir, "square.png"), imageData, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte("notes"), 0o600); err != nil {
		t.Fatal(err)
	}
	return workdir, []ChatAttachment{
		{Name: "notes.md", Kind: "text", MIME: "text/plain; charset=utf-8", Size: 5},
		{Name: "square.png", Kind: "image", MIME: "image/png", Size: int64(len(imageData))},
	}, imageData
}

func redSquarePNG(t *testing.T) []byte {
	t.Helper()
	var data bytes.Buffer
	picture := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			picture.Set(x, y, color.RGBA{R: 255, A: 255})
		}
	}
	if err := png.Encode(&data, picture); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func TestChatAttachmentRuntimePayloadShapes(t *testing.T) {
	workdir, attachments, imageData := deliveryAttachments(t)
	files, err := loadChatAttachmentFiles(workdir, attachments)
	if err != nil {
		t.Fatal(err)
	}
	textPath := files[0].Path
	imagePath := files[1].Path
	if got := promptWithAttachmentPaths("look", files, true); got != "look\nAttached file: "+textPath {
		t.Fatalf("prompt path lines = %q", got)
	}
	if got := attachmentPathLine(filepath.Join(`C:\Users\test`, "attachments", "notes.md")); !strings.HasPrefix(got, "Attached file: ") {
		t.Fatalf("native path line = %q", got)
	}
	if runtimePath := filepath.Join(workdir, "attachments", "notes.md"); !strings.Contains(attachmentPathLine(runtimePath), runtimePath) {
		t.Fatalf("filepath path line = %q", attachmentPathLine(runtimePath))
	}

	claude := claudeUserMessage("look", files)
	claudeContent := claude["content"].([]any)
	if !reflect.DeepEqual(claudeContent, []any{
		map[string]string{"type": "text", "text": "look\nAttached file: " + textPath},
		map[string]any{"type": "image", "source": map[string]string{"type": "base64", "media_type": "image/png", "data": base64.StdEncoding.EncodeToString(imageData)}},
	}) {
		t.Fatalf("Claude stream-json content = %#v", claudeContent)
	}

	codex := codexUserInput("look", files)
	wantCodex := []map[string]string{
		{"type": "text", "text": "look\nAttached file: " + textPath},
		{"type": "localImage", "path": imagePath},
	}
	if !reflect.DeepEqual(codex, wantCodex) {
		t.Fatalf("Codex turn/start input = %#v, want %#v", codex, wantCodex)
	}

	gotCopilot := copilotPromptContent("look", files, true)
	wantCopilot := []any{
		map[string]string{"type": "text", "text": "look\nAttached file: " + textPath},
		map[string]string{"type": "image", "mimeType": "image/png", "data": base64.StdEncoding.EncodeToString(imageData)},
	}
	if !reflect.DeepEqual(gotCopilot, wantCopilot) {
		t.Fatalf("Copilot image prompt = %#v, want %#v", gotCopilot, wantCopilot)
	}
	gotCopilot = copilotPromptContent("look", files, false)
	wantCopilot = []any{map[string]string{"type": "text", "text": "look\nAttached file: " + textPath + "\nAttached file: " + imagePath}}
	if !reflect.DeepEqual(gotCopilot, wantCopilot) {
		t.Fatalf("Copilot text-only prompt = %#v, want %#v", gotCopilot, wantCopilot)
	}

	wantV1 := []any{
		map[string]string{"type": "text", "text": "look\nAttached file: " + textPath},
		map[string]string{"type": "file", "mime": "image/png", "url": chatImageDataURL(files[1])},
	}
	if got := opencodePromptParts("look", files); !reflect.DeepEqual(got, wantV1) {
		t.Fatalf("OpenCode v1 parts = %#v, want %#v", got, wantV1)
	}
	wantV2 := map[string]any{
		"text":  "look\nAttached file: " + textPath,
		"files": []map[string]string{{"uri": chatImageDataURL(files[1]), "mime": "image/png", "name": "square.png"}},
	}
	if got := opencodeV2Prompt("look", files); !reflect.DeepEqual(got, wantV2) {
		t.Fatalf("OpenCode v2 prompt = %#v, want %#v", got, wantV2)
	}
	if files[1].Path != imagePath || !bytes.Equal(files[1].Data, imageData) {
		t.Fatal("attachment read changed image path or bytes")
	}
}

func TestChatAttachmentPathLineUsesNativeFilepath(t *testing.T) {
	workdir := t.TempDir()
	if err := os.Mkdir(filepath.Join(workdir, "attachments"), 0o700); err != nil {
		t.Fatal(err)
	}
	path, err := chatAttachmentAbsolutePath(workdir, "notes.md")
	if err != nil {
		t.Fatal(err)
	}
	line := attachmentPathLine(path)
	if !filepath.IsAbs(path) || !strings.HasPrefix(line, "Attached file: ") || !strings.HasSuffix(line, path) {
		t.Fatalf("native path line = %q", line)
	}
	if filepath.Separator == '\\' && strings.Contains(path, "/") {
		t.Fatalf("Windows path contains slash: %q", path)
	}
}

type captureWriteCloser struct{ bytes.Buffer }

func (w *captureWriteCloser) Close() error { return nil }

func TestClaudeSendWritesImageBlockToStreamJSON(t *testing.T) {
	workdir, attachments, imageData := deliveryAttachments(t)
	files, err := loadChatAttachmentFiles(workdir, attachments)
	if err != nil {
		t.Fatal(err)
	}
	stdin := &captureWriteCloser{}
	driver := &claudeRuntime{workdir: workdir, process: &writeOnlyChatProcess{stdin: stdin}}
	if err := driver.send(ChatSendRequest{Text: "look", Attachments: attachments}); err != nil {
		t.Fatal(err)
	}
	var event struct {
		Type    string `json:"type"`
		Message struct {
			Role    string `json:"role"`
			Content []struct {
				Type   string `json:"type"`
				Text   string `json:"text"`
				Source struct {
					Type      string `json:"type"`
					MediaType string `json:"media_type"`
					Data      string `json:"data"`
				} `json:"source"`
			} `json:"content"`
		} `json:"message"`
	}
	line := strings.TrimSpace(stdin.String())
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		t.Fatal(err)
	}
	if event.Type != "user" || event.Message.Role != "user" || len(event.Message.Content) != 2 || event.Message.Content[0].Text != "look\nAttached file: "+files[0].Path || event.Message.Content[1].Type != "image" || event.Message.Content[1].Source.Type != "base64" || event.Message.Content[1].Source.MediaType != "image/png" || event.Message.Content[1].Source.Data != base64.StdEncoding.EncodeToString(imageData) {
		t.Fatalf("Claude event = %#v", event)
	}
}

type writeOnlyChatProcess struct{ stdin io.WriteCloser }

func (p *writeOnlyChatProcess) Stdin() io.WriteCloser { return p.stdin }
func (*writeOnlyChatProcess) Stdout() io.ReadCloser   { return nil }
func (*writeOnlyChatProcess) Stderr() io.ReadCloser   { return nil }
func (*writeOnlyChatProcess) Interrupt() error        { return nil }
func (*writeOnlyChatProcess) Kill() error             { return nil }
func (*writeOnlyChatProcess) Wait() error             { return nil }

type attachmentCodexClient struct{ params map[string]any }

func (c *attachmentCodexClient) Call(_ context.Context, method string, params, result any) error {
	if method != "turn/start" {
		return fmt.Errorf("unexpected method %q", method)
	}
	encoded, _ := json.Marshal(params)
	if err := json.Unmarshal(encoded, &c.params); err != nil {
		return err
	}
	return json.Unmarshal([]byte(`{"turn":{"id":"turn-1"}}`), result)
}
func (*attachmentCodexClient) Models(context.Context) ([]connectorcli.Model, error) { return nil, nil }
func (*attachmentCodexClient) Notifications() <-chan connectorcli.Notification      { return nil }
func (*attachmentCodexClient) Requests() <-chan connectorcli.Request                { return nil }
func (*attachmentCodexClient) Respond(json.RawMessage, any) error                   { return nil }
func (*attachmentCodexClient) RespondError(json.RawMessage, int, string) error      { return nil }
func (*attachmentCodexClient) Close() error                                         { return nil }

func TestCodexSendWritesLocalImageInputItem(t *testing.T) {
	database, _, root := chatAPI(t)
	connectorID := createTestConnector(t, database)
	chatID := createDriverTestChat(t, database, connectorID, root)
	if _, err := database.Exec(`UPDATE chat SET thinking=1 WHERE id=?`, chatID); err != nil {
		t.Fatal(err)
	}
	workdir, attachments, _ := deliveryAttachments(t)
	files, err := loadChatAttachmentFiles(workdir, attachments)
	if err != nil {
		t.Fatal(err)
	}
	client := &attachmentCodexClient{}
	driver := &codexRuntime{manager: newChatManager(database, Options{}), chatID: chatID, workdir: workdir, client: client, threadID: "thread"}
	if err := driver.send(ChatSendRequest{Text: "look", Attachments: attachments}); err != nil {
		t.Fatal(err)
	}
	input, _ := client.params["input"].([]any)
	wantText := "look\nAttached file: " + files[0].Path
	if len(input) != 2 {
		t.Fatalf("Codex turn/start payload = %#v", client.params)
	}
	textItem, imageItem := input[0].(map[string]any), input[1].(map[string]any)
	if textItem["type"] != "text" || textItem["text"] != wantText || imageItem["type"] != "localImage" || !strings.HasSuffix(imageItem["path"].(string), filepath.Join("attachments", "square.png")) {
		t.Fatalf("Codex turn/start payload = %#v", client.params)
	}
}

func TestCopilotImageCapabilityAndFallbackNote(t *testing.T) {
	for _, test := range []struct {
		name string
		init string
		want bool
	}{
		{"supported", `{"agentCapabilities":{"promptCapabilities":{"image":true}}}`, true},
		{"omitted", `{"agentCapabilities":{"promptCapabilities":{"embeddedContext":true}}}`, false},
		{"explicit false", `{"agentCapabilities":{"promptCapabilities":{"image":false}}}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := copilotImageCapability(json.RawMessage(test.init))
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("ACP image capability = %v, want %v", got, test.want)
			}
		})
	}
	database, _, root := chatAPI(t)
	connectorID := createCopilotTestConnector(t, database)
	chatID := createDriverTestChat(t, database, connectorID, root)
	var emitted any
	manager := newChatManager(database, Options{Emit: func(_ string, event any) { emitted = event }})
	message := "Copilot can't take images; sent the file path instead"
	if err := saveChatImageNote(manager, chatID, message); err != nil {
		t.Fatal(err)
	}
	var role, content string
	if err := database.QueryRow(`SELECT role,content FROM chat_message WHERE chat_id=?`, chatID).Scan(&role, &content); err != nil || role != "tool" || content != message {
		t.Fatalf("visible Copilot note = %q %q, %v", role, content, err)
	}
	if event, ok := emitted.(map[string]any); !ok || event["type"] != "tool" {
		t.Fatalf("Copilot note event = %#v", emitted)
	}
}

func TestLocalOpenAIImageAcceptAndUnsupportedRetry(t *testing.T) {
	for _, reject := range []bool{false, true} {
		name := "accepted"
		if reject {
			name = "unsupported retry"
		}
		t.Run(name, func(t *testing.T) {
			var calls atomic.Int32
			var firstContent, retryContent any
			database, _, chatID, server, events := newLocalChatFixture(t, func(w http.ResponseWriter, _ *http.Request, request map[string]any) {
				call := calls.Add(1)
				messages := request["messages"].([]any)
				last := messages[len(messages)-1].(map[string]any)
				if call == 1 {
					firstContent = last["content"]
					if reject {
						w.WriteHeader(http.StatusBadRequest)
						_, _ = io.WriteString(w, `{"error":"image input is unsupported"}`)
						return
					}
				} else {
					retryContent = last["content"]
				}
				writeSSE(w, `{"choices":[{"delta":{"content":"red"}}]}`)
			})
			defer server.Close()
			response := postAttachmentFiles(t, server, chatID, []attachmentUpload{{"square.png", redSquarePNG(t)}})
			var uploaded []ChatAttachment
			if response.Code != http.StatusCreated || json.Unmarshal(response.Body.Bytes(), &uploaded) != nil {
				t.Fatalf("upload = %d %s", response.Code, response.Body.String())
			}
			if code := requestJSON(t, server, http.MethodPost, "/api/chats/"+chatID+"/messages", map[string]any{"text": "What color?", "attachments": []string{uploaded[0].Name}}, nil); code != http.StatusAccepted {
				t.Fatalf("send = %d", code)
			}
			waitLocalDone(t, events)
			if calls.Load() != 1 && (!reject || calls.Load() != 2) {
				t.Fatalf("completion calls = %d", calls.Load())
			}
			if !reject {
				content, ok := firstContent.([]any)
				if !ok || len(content) != 2 || !strings.HasPrefix(content[0].(map[string]any)["text"].(string), "What color? /no_think") {
					t.Fatalf("OpenAI image content = %#v", firstContent)
				}
				imagePart := content[1].(map[string]any)
				imageURL := imagePart["image_url"].(map[string]any)["url"].(string)
				if !strings.HasPrefix(imageURL, "data:image/png;base64,") {
					t.Fatalf("OpenAI data URL = %q", imageURL)
				}
				return
			}
			if _, ok := firstContent.([]any); !ok {
				t.Fatalf("first OpenAI content did not carry image: %#v", firstContent)
			}
			fallback, ok := retryContent.(string)
			if !ok || !strings.Contains(fallback, "Attached file: ") || !strings.Contains(fallback, filepath.Join("attachments", "square.png")) {
				t.Fatalf("OpenAI retry content = %#v", retryContent)
			}
			var role, note string
			if err := database.QueryRow(`SELECT role,content FROM chat_message WHERE chat_id=? AND content=?`, chatID, localImageUnsupportedNote).Scan(&role, &note); err != nil || role != "tool" || note != localImageUnsupportedNote {
				t.Fatalf("fallback note = %q %q, %v", role, note, err)
			}
		})
	}
}

func TestOllamaImageCapabilityAndNativePayload(t *testing.T) {
	for _, vision := range []bool{true, false} {
		t.Run(fmt.Sprintf("vision=%v", vision), func(t *testing.T) {
			var chatBody map[string]any
			var showCalls atomic.Int32
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/show":
					showCalls.Add(1)
					caps := []string{"completion"}
					if vision {
						caps = append(caps, "vision")
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"capabilities": caps})
				case "/api/chat":
					if err := json.NewDecoder(r.Body).Decode(&chatBody); err != nil {
						t.Error(err)
					}
					_, _ = io.WriteString(w, `{"message":{"content":"red"},"done":true}`+"\n")
				default:
					http.NotFound(w, r)
				}
			}))
			defer fake.Close()
			workdir, attachments, imageData := deliveryAttachments(t)
			cacheKey := fake.URL + "\x00model-test"
			localModelCapabilities.Delete(cacheKey)
			manager := newChatManager(nil, Options{})
			driver := &localRuntime{manager: manager, chatID: "chat", model: "model-test", ollama: true, ollamaURL: fake.URL, client: fake.Client(), workdir: workdir}
			files, err := loadChatAttachmentFiles(workdir, attachments[1:])
			if err != nil {
				t.Fatal(err)
			}
			gotVision := driver.ollamaVision(context.Background())
			toSend := files
			if !vision {
				toSend = nil
			}
			result, err := driver.streamOllamaWithImages(context.Background(), []map[string]any{{"role": "user", "content": "what color"}}, false, true, toSend)
			if err != nil || result.text != "red" {
				t.Fatalf("Ollama stream = %#v, %v", result, err)
			}
			if gotVision != vision || showCalls.Load() != 1 {
				t.Fatalf("vision capability=%v show calls=%d", gotVision, showCalls.Load())
			}
			messages := chatBody["messages"].([]any)
			last := messages[len(messages)-1].(map[string]any)
			images, hasImages := last["images"].([]any)
			if vision {
				if !hasImages || len(images) != 1 || images[0] != base64.StdEncoding.EncodeToString(imageData) {
					t.Fatalf("Ollama native images = %#v", last["images"])
				}
			} else if hasImages {
				t.Fatalf("text-only Ollama got images: %#v", images)
			}
		})
	}
}

func TestOllamaWithoutVisionStoresNoteAndSendsPathOnly(t *testing.T) {
	database, _, root := chatAPI(t)
	connectorID := createTestConnector(t, database)
	chatID := createDriverTestChat(t, database, connectorID, root)
	workdir := filepath.Join(root, "chats", chatID)
	attachmentDir := filepath.Join(workdir, "attachments")
	if err := os.MkdirAll(attachmentDir, 0o700); err != nil {
		t.Fatal(err)
	}
	imageData := redSquarePNG(t)
	if err := os.WriteFile(filepath.Join(attachmentDir, "square.png"), imageData, 0o600); err != nil {
		t.Fatal(err)
	}
	attachment := ChatAttachment{Name: "square.png", Kind: "image", MIME: "image/png", Size: int64(len(imageData))}
	if _, err := InsertChatMessageWithAttachments(database, chatID, "user", "What color?", []ChatAttachment{attachment}); err != nil {
		t.Fatal(err)
	}
	var requestBody map[string]any
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/show":
			_ = json.NewEncoder(w).Encode(map[string]any{"capabilities": []string{"completion"}})
		case "/api/chat":
			if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
				t.Error(err)
			}
			_, _ = io.WriteString(w, `{"message":{"content":"red"},"done":true}`+"\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer fake.Close()
	manager := newChatManager(database, Options{})
	driver := &localRuntime{manager: manager, chatID: chatID, model: "text-only", ollama: true, ollamaURL: fake.URL, client: fake.Client(), workdir: workdir}
	text, err := driver.completeTurn(context.Background(), ChatSendRequest{Text: "What color?", Attachments: []ChatAttachment{attachment}})
	if err != nil || text != "red" {
		t.Fatalf("Ollama turn = %q, %v", text, err)
	}
	messages := requestBody["messages"].([]any)
	last := messages[len(messages)-1].(map[string]any)
	imagePath, err := chatAttachmentAbsolutePath(workdir, "square.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := last["images"]; exists || !strings.Contains(last["content"].(string), attachmentPathLine(imagePath)) {
		t.Fatalf("text-only Ollama payload = %#v", last)
	}
	var role, note string
	if err := database.QueryRow(`SELECT role,content FROM chat_message WHERE chat_id=? AND content=?`, chatID, localImageUnsupportedNote).Scan(&role, &note); err != nil || role != "tool" || note != localImageUnsupportedNote {
		t.Fatalf("Ollama image note = %q %q, %v", role, note, err)
	}
}

func TestLocalHistoryIncludesOldAttachmentPathButDoesNotResendImage(t *testing.T) {
	database, _, root := chatAPI(t)
	connectorID := createTestConnector(t, database)
	chatID := createDriverTestChat(t, database, connectorID, root)
	workdir := filepath.Join(root, "chats", chatID)
	if err := os.MkdirAll(filepath.Join(workdir, "attachments"), 0o700); err != nil {
		t.Fatal(err)
	}
	imageData := redSquarePNG(t)
	if err := os.WriteFile(filepath.Join(workdir, "attachments", "old.png"), imageData, 0o600); err != nil {
		t.Fatal(err)
	}
	old := ChatAttachment{Name: "old.png", Kind: "image", MIME: "image/png", Size: int64(len(imageData))}
	encoded, _ := json.Marshal([]ChatAttachment{old})
	if _, err := database.Exec(`INSERT INTO chat_message(id,chat_id,role,content,created_at,attachments) VALUES(?,?,?,?,?,?)`, uid(), chatID, "user", "old turn", "2026-01-01T00:00:00Z", string(encoded)); err != nil {
		t.Fatal(err)
	}
	if _, err := InsertChatMessage(database, chatID, "user", "new turn"); err != nil {
		t.Fatal(err)
	}
	driver := &localRuntime{manager: newChatManager(database, Options{}), chatID: chatID, workdir: workdir}
	messages, err := driver.historyForToolsWithAttachments(true)
	if err != nil {
		t.Fatal(err)
	}
	var oldMessage, currentMessage map[string]any
	for _, message := range messages {
		if message["content"] == "new turn" {
			currentMessage = message
		}
		if strings.HasPrefix(message["content"].(string), "old turn") {
			oldMessage = message
		}
	}
	if currentMessage == nil || currentMessage["content"] != "new turn" {
		t.Fatalf("current turn content = %#v", currentMessage)
	}
	oldPath, err := chatAttachmentAbsolutePath(workdir, "old.png")
	if err != nil {
		t.Fatal(err)
	}
	if oldMessage == nil || !strings.Contains(oldMessage["content"].(string), attachmentPathLine(oldPath)) {
		t.Fatalf("old attachment path missing from history: %#v", oldMessage)
	}
	openAI := openAIMessagesWithImages(messages, true, nil)
	encodedHistory, _ := json.Marshal(openAI)
	if strings.Contains(string(encodedHistory), "data:image/png") || strings.Contains(string(encodedHistory), base64.StdEncoding.EncodeToString(imageData)) {
		t.Fatalf("old image resent in history: %s", encodedHistory)
	}
}
