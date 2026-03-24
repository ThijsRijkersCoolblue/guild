package tui

import (
	"context"
	"fmt"
	"guild/llm"
	"guild/prompt"
	"strings"
)

// ProgressKind describes the type of a progress event.
type ProgressKind string

const (
	ProgressThinking  ProgressKind = "thinking"
	ProgressReading   ProgressKind = "reading"
	ProgressWriting   ProgressKind = "writing"
	ProgressUpdating  ProgressKind = "updating"
	ProgressDone      ProgressKind = "done"
	ProgressError     ProgressKind = "error"
)

// ProgressEvent is emitted by agentAsk to describe what the agent is doing right now.
type ProgressEvent struct {
	Kind    ProgressKind
	Detail  string // file path, error message, or reasoning snippet
	Attempt int    // which loop iteration (1-based)
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

	var completedActions []string
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
			if len(completedActions) > 0 {
				finalText = strings.Join(completedActions, "\n") + "\n\n" + finalText
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
				conversation += fmt.Sprintf("assistant: %s\n\nsystem: Could not read %s: %v. Try a different path.\n\n", text, a.Path, err)
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
				conversation += fmt.Sprintf("assistant: %s\n\nsystem: write_file failed: %v. Try again.\n\n", text, err)
			} else {
				completedActions = append(completedActions, fmt.Sprintf("written to %s", a.Path))
				onFileWritten()
				tail := fmt.Sprintf("assistant: %s\n\nsystem: Successfully written to %s. If you have more actions to perform, do them now. Otherwise respond with a plain summary of what you did.\n\n", text, a.Path)
				conversation = historyToPrompt(*systemPrompt, history) + tail
			}

		case "replace_in_file":
			emit(ProgressUpdating, a.Path, n)
			existing, err := prompt.ReadFile(a.Path)
			if err != nil {
				emit(ProgressError, fmt.Sprintf("read %s: %v", a.Path, err), n)
				conversation += fmt.Sprintf("assistant: %s\n\nsystem: Could not read %s: %v\n\n", text, a.Path, err)
			} else if !strings.Contains(existing, a.Old) {
				emit(ProgressError, fmt.Sprintf("old string not found in %s", a.Path), n)
				conversation += fmt.Sprintf(
					"assistant: %s\n\nsystem: replace_in_file failed — exact \"old\" string not found in %s. Use write_file with the full corrected content instead.\n\nCurrent file:\n```\n%s\n```\n\n",
					text, a.Path, existing,
				)
			} else if err := ReplaceInFile(a.Path, a.Old, a.New); err != nil {
				emit(ProgressError, fmt.Sprintf("replace %s: %v", a.Path, err), n)
				conversation += fmt.Sprintf("assistant: %s\n\nsystem: replace_in_file failed: %v\n\n", text, err)
			} else {
				completedActions = append(completedActions, fmt.Sprintf("updated %s", a.Path))
				onFileWritten()
				tail := fmt.Sprintf("assistant: %s\n\nsystem: Successfully updated %s. If you have more actions to perform, do them now. Otherwise respond with a plain summary of what you did.\n\n", text, a.Path)
				conversation = historyToPrompt(*systemPrompt, history) + tail
			}

		default:
			emit(ProgressDone, "", n)
			return StripActions(response), nil
		}
	}

	emit(ProgressError, "exceeded maximum attempts", 10)
	return "", fmt.Errorf("could not complete the change after multiple attempts")
}
