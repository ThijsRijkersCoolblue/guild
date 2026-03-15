package tui

import (
	"context"
	"fmt"
	"guild/llm"
	"guild/prompt"
	"strings"

	"github.com/rivo/tview"
)

func agentAsk(
	ctx context.Context,
	client llm.LLM,
	systemPrompt *string,
	history []turn,
	statusBar *tview.TextView,
	app *tview.Application,
	onFileWritten func(),
) (string, error) {
	// Build prompt from full history so the model has context of past turns
	conversation := historyToPrompt(*systemPrompt, history)
	var completedActions []string

	for range 10 {
		response, err := client.Ask(ctx, conversation)
		if err != nil {
			return "", err
		}

		a := ParseAction(response)
		if a == nil {
			// No more actions — build final response with action summaries prepended
			finalText := StripActions(response)
			if len(completedActions) > 0 {
				finalText = strings.Join(completedActions, "\n") + "\n\n" + finalText
			}
			return strings.TrimSpace(finalText), nil
		}

		text := StripActions(response)

		switch a.Type {
		case "read_file":
			app.QueueUpdateDraw(func() {
				statusBar.SetText(fmt.Sprintf("  [#ffcb6b]reading %s...[-]", a.Path))
			})
			fileContent, err := prompt.ReadFile(a.Path)
			if err != nil {
				conversation += fmt.Sprintf("assistant: %s\n\nsystem: Could not read %s: %v. Try a different path.\n\n", text, a.Path, err)
			} else {
				conversation += fmt.Sprintf(
					"assistant: %s\n\nsystem: Contents of %s:\n```\n%s\n```\nNow apply the change using write_file.\n\n",
					text, a.Path, fileContent,
				)
			}

		case "write_file":
			app.QueueUpdateDraw(func() {
				statusBar.SetText(fmt.Sprintf("  [#ffcb6b]writing %s...[-]", a.Path))
			})
			if err := WriteFile(a.Path, a.Content); err != nil {
				conversation += fmt.Sprintf("assistant: %s\n\nsystem: write_file failed: %v. Try again.\n\n", text, err)
			} else {
				// Success — feed result back and keep looping so model can do follow-up actions
				completedActions = append(completedActions, fmt.Sprintf("written to %s", a.Path))
				onFileWritten()
				conversation += fmt.Sprintf("assistant: %s\n\nsystem: ✅ Successfully written to %s. If you have more actions to perform, do them now. Otherwise respond with a plain summary of what you did.\n\n", text, a.Path)
			}

		case "replace_in_file":
			app.QueueUpdateDraw(func() {
				statusBar.SetText(fmt.Sprintf("  [#ffcb6b]updating %s...[-]", a.Path))
			})
			existing, err := prompt.ReadFile(a.Path)
			if err != nil {
				conversation += fmt.Sprintf("assistant: %s\n\nsystem: Could not read %s: %v\n\n", text, a.Path, err)
			} else if !strings.Contains(existing, a.Old) {
				conversation += fmt.Sprintf(
					"assistant: %s\n\nsystem: replace_in_file failed — exact \"old\" string not found in %s. Use write_file with the full corrected content instead.\n\nCurrent file:\n```\n%s\n```\n\n",
					text, a.Path, existing,
				)
			} else if err := ReplaceInFile(a.Path, a.Old, a.New); err != nil {
				conversation += fmt.Sprintf("assistant: %s\n\nsystem: replace_in_file failed: %v\n\n", text, err)
			} else {
				// Success — keep looping for follow-up actions
				completedActions = append(completedActions, fmt.Sprintf("updated %s", a.Path))
				onFileWritten()
				conversation += fmt.Sprintf("assistant: %s\n\nsystem: ✅ Successfully updated %s. If you have more actions to perform, do them now. Otherwise respond with a plain summary of what you did.\n\n", text, a.Path)
			}

		default:
			return StripActions(response), nil
		}
	}

	return "", fmt.Errorf("could not complete the change after multiple attempts")
}
