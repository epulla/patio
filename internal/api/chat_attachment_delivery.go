package api

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const localImageUnsupportedNote = "This model can't see images"

type chatAttachmentFile struct {
	ChatAttachment
	Path string
	Data []byte
}

func loadChatAttachmentFiles(workdir string, attachments []ChatAttachment) ([]chatAttachmentFile, error) {
	if len(attachments) == 0 {
		return nil, nil
	}
	dir, err := chatAttachmentsDir(workdir, false)
	if err != nil {
		return nil, err
	}
	files := make([]chatAttachmentFile, 0, len(attachments))
	for _, attachment := range attachments {
		if !validChatAttachmentName(attachment.Name) {
			return nil, errChatAttachmentName
		}
		path, err := filepath.Abs(filepath.Join(dir, attachment.Name))
		if err != nil {
			return nil, err
		}
		inside, err := pathWithin(dir, path)
		if err != nil || !inside {
			return nil, errChatAttachmentUnsafe
		}
		info, err := os.Lstat(path)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, errChatAttachmentUnsafe
		}
		file := chatAttachmentFile{ChatAttachment: attachment, Path: path}
		if attachment.Kind == "image" {
			if info.Size() > maxChatImageAttachment {
				return nil, errChatAttachmentTooLarge
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			if int64(len(data)) > maxChatImageAttachment {
				return nil, errChatAttachmentTooLarge
			}
			file.Data = data
		}
		files = append(files, file)
	}
	return files, nil
}

func attachmentPathLine(path string) string { return "Attached file: " + path }

func chatAttachmentAbsolutePath(workdir, name string) (string, error) {
	if !validChatAttachmentName(name) {
		return "", errChatAttachmentName
	}
	dir, err := chatAttachmentsDir(workdir, false)
	if err != nil {
		return "", err
	}
	path, err := filepath.Abs(filepath.Join(dir, name))
	if err != nil {
		return "", err
	}
	inside, err := pathWithin(dir, path)
	if err != nil || !inside {
		return "", errChatAttachmentUnsafe
	}
	return path, nil
}

func promptWithAttachmentPaths(text string, files []chatAttachmentFile, includeImages bool) string {
	var lines []string
	for _, file := range files {
		if file.Kind != "image" || !includeImages {
			lines = append(lines, attachmentPathLine(file.Path))
		}
	}
	return appendPromptLines(text, lines)
}

func appendPromptLines(text string, lines []string) string {
	if len(lines) == 0 {
		return text
	}
	if text != "" {
		text += "\n"
	}
	return text + strings.Join(lines, "\n")
}

func appendAttachmentPathsToMessages(messages []map[string]any, attachments []ChatAttachment, workdir string) error {
	if len(attachments) == 0 {
		return nil
	}
	files, err := loadChatAttachmentFiles(workdir, attachments)
	if err != nil {
		return err
	}
	lines := make([]string, 0, len(files))
	for _, file := range files {
		lines = append(lines, attachmentPathLine(file.Path))
	}
	if len(messages) == 0 {
		return errors.New("local history has no user prompt")
	}
	last := messages[len(messages)-1]
	content, ok := last["content"].(string)
	if !ok {
		return fmt.Errorf("local history user content has type %T", last["content"])
	}
	last["content"] = appendPromptLines(content, lines)
	return nil
}

func chatImageDataURL(file chatAttachmentFile) string {
	return "data:" + file.MIME + ";base64," + base64.StdEncoding.EncodeToString(file.Data)
}

func saveChatImageNote(manager *chatManager, chatID, text string) error {
	return manager.emitTool(chatID, text)
}
