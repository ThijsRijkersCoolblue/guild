package tui

import (
	"context"
	"fmt"
	"guild/llm"
	"guild/prompt"
	"strings"
)

type ProgressKind string

const (
	ProgressThinking ProgressKind = "thinking"
	ProgressReading  ProgressKind = "reading"
	ProgressWriting  ProgressKind = "writing"
	ProgressUpdating ProgressKind = "updating"
	ProgressDone     ProgressKind = "done"
	ProgressError    ProgressKind = "error"
)

type ProgressEvent struct {
	Kind    ProgressKind
	Detail  string
	Attempt int
}

const fileContentMarker = "system: Contents of "

func evictFileContent(conversation, path string) string {
	startMarker := fmt.Sprintf("system: Contents of %s:\n```\n", path)
	startIdx := strings.Index(conversation, startMarker)
	if startIdx == -1 {
		return conversation
	}
	endMarker := "\n```\n"
	endIdx := strings.Index(conversation[startIdx+len(startMarker):], endMarker)
	if endIdx == -1 {
		return conversation
	}
	endIdx += startIdx + len(startMarker) + len(endMarker)

	replacement := fmt.Sprintf("system: [contents of %s loaded and applied]\n", path)
	return conversation[:startIdx] + replacement + conversation[endIdx:]
}

func agentAsk(
	ctx context.Context,
	client llm.LLM,
	systemPrompt *string,
	history []turn,
	onFileWritten func(),
	onProgress func(ProgressEvent),
) (string, error) {
	emit := func(kind ProgressKind, detail string, attempt int) {
		if onProgress != nil {
			onProgress(ProgressEvent{Kind: kind, Detail: detail, Attempt: attempt})
		}
	}

	// actionLog tracks a short human-readable summary of completed operations.
	// It is appended to the final response so the caller sees what was done.
	var actionLog []string

	// conversation is built once from history and extended incrementally.
	// We never rebuild it from scratch after a successful write, instead we
	// append a compact tail and evict stale file content, keeping the window small.
	conversation := historyToPrompt(*systemPrompt, history)

	for attempt := range 10 {
		n := attempt + 1

		emit(ProgressThinking, "calling model…", n)

		response, err := client.Ask(ctx, conversation)
		if err != nil {
			emit(ProgressError, err.Error(), n)
			return "", err
		}

		// Emit a short reasoning preview (first 120 chars of stripped text).
		preview := strings.TrimSpace(StripActions(response))
		if len(preview) > 120 {
			preview = preview[:120] + "…"
		}
		if preview != "" {
			emit(ProgressThinking, preview, n)
		}

		a := ParseAction(response)
		if a == nil {
			finalText := StripActions(response)
			if len(actionLog) > 0 {
				finalText = strings.Join(actionLog, "\n") + "\n\n" + finalText
			}
			emit(ProgressDone, "", n)
			return strings.TrimSpace(finalText), nil
		}

		text := StripActions(response)

		switch a.Type {
		case "read_file":
			emit(ProgressReading, a.Path, n)
			fileContent, err := prompt.ReadFile(a.Path)
			if err != nil {
				emit(ProgressError, fmt.Sprintf("read %s: %v", a.Path, err), n)
				conversation += fmt.Sprintf(
					"assistant: %s\n\nsystem: Could not read %s: %v. Try a different path.\n\n",
					text, a.Path, err,
				)
			} else {
				conversation += fmt.Sprintf(
					"assistant: %s\n\nsystem: Contents of %s:\n```\n%s\n```\nNow apply the change using write_file.\n\n",
					text, a.Path, fileContent,
				)
			}

		case "write_file":
			emit(ProgressWriting, a.Path, n)
			if err := WriteFile(a.Path, a.Content); err != nil {
				emit(ProgressError, fmt.Sprintf("write %s: %v", a.Path, err), n)
				conversation += fmt.Sprintf(
					"assistant: %s\n\nsystem: write_file failed: %v. Try again.\n\n",
					text, err,
				)
			} else {
				actionLog = append(actionLog, fmt.Sprintf("written to %s", a.Path))
				onFileWritten()

				// Evict the now stale file contents from the conversation so they
				// don't keep consuming tokens on subsequent iterations.
				conversation = evictFileContent(conversation, a.Path)

				// Append a compact tail, do NOT rebuild from full history.
				conversation += fmt.Sprintf(
					"assistant: %s\n\nsystem: Successfully written to %s. If you have more files to change, do them now. Otherwise respond with a brief summary.\n\n",
					text, a.Path,
				)
			}

		case "replace_in_file":
			emit(ProgressUpdating, a.Path, n)
			existing, err := prompt.ReadFile(a.Path)
			if err != nil {
				emit(ProgressError, fmt.Sprintf("read %s: %v", a.Path, err), n)
				conversation += fmt.Sprintf(
					"assistant: %s\n\nsystem: Could not read %s: %v\n\n",
					text, a.Path, err,
				)
			} else if !strings.Contains(existing, a.Old) {
				emit(ProgressError, fmt.Sprintf("old string not found in %s", a.Path), n)
				conversation += fmt.Sprintf(
					"assistant: %s\n\nsystem: replace_in_file failed — exact \"old\" string not found in %s. Use write_file with the full corrected content instead.\n\nCurrent file:\n```\n%s\n```\n\n",
					text, a.Path, existing,
				)
			} else if err := ReplaceInFile(a.Path, a.Old, a.New); err != nil {
				emit(ProgressError, fmt.Sprintf("replace %s: %v", a.Path, err), n)
				conversation += fmt.Sprintf(
					"assistant: %s\n\nsystem: replace_in_file failed: %v\n\n",
					text, err,
				)
			} else {
				actionLog = append(actionLog, fmt.Sprintf("updated %s", a.Path))
				onFileWritten()

				// Evict the stale file contents loaded for this replace operation.
				conversation = evictFileContent(conversation, a.Path)

				// Append a compact tail, do NOT rebuild from full history.
				conversation += fmt.Sprintf(
					"assistant: %s\n\nsystem: Successfully updated %s. If you have more files to change, do them now. Otherwise respond with a brief summary.\n\n",
					text, a.Path,
				)
			}

		default:
			emit(ProgressDone, "", n)
			return StripActions(response), nil
		}
	}

	emit(ProgressError, "exceeded maximum attempts", 10)
	return "", fmt.Errorf("could not complete the change after multiple attempts")
}
