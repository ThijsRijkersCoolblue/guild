package tui

import (
	"context"
	"fmt"
	"guild/llm"
	"guild/prompt"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

var (
	bgMain      = tcell.GetColor("#242932") // darker than nord0
	bgInput     = tcell.GetColor("#2e3440") // nord0 - lifted input
	bgSidebar   = tcell.GetColor("#1c2028") // very dark sidebar
	fgText      = tcell.GetColor("#eceff4") // nord6 - crisp white text
	fgMuted     = tcell.GetColor("#4c566a") // nord3 - muted hints
	fgGreen     = tcell.GetColor("#549897") // deeper frost teal (assistant)
	fgRed       = tcell.GetColor("#bf616a") // nord11 - red (errors)
	fgPurple    = tcell.GetColor("#5b7fa6") // deeper frost blue (user)
	bgBorder    = tcell.GetColor("#485265") // nord0 - subtle border
	bgHighlight = tcell.GetColor("#373f4f") // dark selection
	fgCode      = tcell.GetColor("#c6dae1") // light blue code text
	fgOrange    = tcell.GetColor("#d08770") // nord12 - orange for model indicator
)

type turn struct {
	role    string // "user" or "assistant"
	content string
}

func historyToPrompt(systemPrompt string, history []turn) string {
	var sb strings.Builder
	sb.WriteString(systemPrompt)
	sb.WriteString("\n\n")
	for _, t := range history {
		sb.WriteString(t.role)
		sb.WriteString(": ")
		sb.WriteString(t.content)
		sb.WriteString("\n\n")
	}
	return sb.String()
}

func renderCodeBlocks(text string) (string, string) {
	lastCode := ""
	result := CodeBlockRegex.ReplaceAllStringFunc(text, func(match string) string {
		groups := CodeBlockRegex.FindStringSubmatch(match)
		if len(groups) < 2 {
			return match
		}
		code := strings.TrimSpace(groups[1])
		lastCode = code
		lines := strings.Split(code, "\n")
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("\n[%s]  ╔═ code ══════════════════════════════════[-]\n", fgGreen.CSS()))
		for _, line := range lines {
			sb.WriteString(fmt.Sprintf("[%s]  ║[-] [%s]%s[-]\n", fgGreen.CSS(), fgCode.CSS(), line))
		}
		sb.WriteString(fmt.Sprintf("[%s]  ╚═ ctrl+y to copy ═══════════════════════[-]\n", fgGreen.CSS()))
		return sb.String()
	})
	return result, lastCode
}

func formatMessage(role, text string) string {
	var roleTag string
	switch role {
	case "user":
		roleTag = fmt.Sprintf("[%s]> you[-]", fgPurple.CSS())
	case "assistant":
		roleTag = fmt.Sprintf("[%s]> guild[-]", fgOrange.CSS())
	case "error":
		roleTag = fmt.Sprintf("[%s]> error[-]", fgRed.CSS())
	default:
		roleTag = fmt.Sprintf("[%s]> %s[-]", fgMuted.CSS(), role)
	}
	header := roleTag + "\n"
	body := "  " + strings.ReplaceAll(text, "\n", "\n  ") + "\n"
	divider := fmt.Sprintf("[%s]────────────────────────────────────────[-]\n", bgBorder.CSS())
	return header + body + divider
}

func formatAssistantMessage(text string) (string, string) {
	rendered, lastCode := renderCodeBlocks(text)
	header := fmt.Sprintf("[%s]> guild[-]\n", fgOrange.CSS())
	body := "  " + strings.ReplaceAll(rendered, "\n", "\n  ") + "\n"
	divider := fmt.Sprintf("[%s]────────────────────────────────────────[-]\n", bgBorder.CSS())
	return header + body + divider, lastCode
}

func updateChat(view *tview.TextView, messages []string) {
	view.Clear()
	for _, msg := range messages {
		fmt.Fprint(view, msg)
	}
	view.ScrollToEnd()
}

func modelindicator() string {
	model := os.Getenv("LLM_MODEL")
	if model == "" {
		model = "unknown"
	}
	return fmt.Sprintf("[%s]Model:[-] [%s]%s[-]", fgMuted.CSS(), fgOrange.CSS(), model)
}

const statusDefaultFmt = "  [#4c566a]ctrl+c[-] quit   [#4c566a]ctrl+l[-] clear   [#4c566a]ctrl+y[-] copy code"

func statusDefault() string {
	return statusDefaultFmt + "   [#eceff4]│[-]   " + modelindicator()
}

func StartChat(parentCtx context.Context, client llm.LLM) {
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	// Force true color in WSL — tcell often fails to detect it automatically
	if os.Getenv("COLORTERM") == "" {
		os.Setenv("COLORTERM", "truecolor")
	}
	if os.Getenv("TERM") == "" {
		os.Setenv("TERM", "xterm-256color")
	}

	entries, err := prompt.BuildFileList(".")
	if err != nil {
		log.Fatalf("could not scan project: %v", err)
	}
	systemPromptStr := prompt.Build(entries)
	systemPrompt := &systemPromptStr

	app := tview.NewApplication()

	chatView := tview.NewTextView()
	chatView.
		SetDynamicColors(true).
		SetScrollable(true).
		SetWrap(true).
		SetWordWrap(true)
	chatView.SetBackgroundColor(bgMain)
	chatView.SetTextColor(fgText)
	chatView.SetChangedFunc(func() { app.Draw() })

	inputField := tview.NewInputField().
		SetFieldWidth(0).
		SetFieldBackgroundColor(bgInput).
		SetFieldTextColor(fgText)
	inputField.SetBackgroundColor(bgInput)

	statusBar := tview.NewTextView()
	statusBar.SetDynamicColors(true)
	statusBar.SetBackgroundColor(bgInput)
	statusBar.SetText(statusDefault())

	messages := []string{
		fmt.Sprintf("[%s]\n  ██████╗ ██╗   ██╗██╗██╗     ██████╗\n ██╔════╝ ██║   ██║██║██║     ██╔══██╗\n ██║  ███╗██║   ██║██║██║     ██║  ██║\n ██║   ██║██║   ██║██║██║     ██║  ██║\n ╚██████╔╝╚██████╔╝██║███████╗██████╔╝\n  ╚═════╝  ╚═════╝ ╚═╝╚══════╝╚═════╝", fgGreen.CSS()),
		fmt.Sprintf("[%s] \n Loaded %d project files into context.\n Type a message and press Enter.\n[-]\n", fgMuted.CSS(), len(entries)),
	}
	updateChat(chatView, messages)

	var history []turn
	var mu sync.Mutex
	var lastCodeBlock string

	inputFlex := tview.NewFlex().
		SetDirection(tview.FlexColumn).
		AddItem(nil, 1, 0, false).
		AddItem(inputField, 0, 1, true).
		AddItem(nil, 2, 0, false)
	inputFlex.SetBackgroundColor(bgInput)

	statusFlex := tview.NewFlex().
		SetDirection(tview.FlexColumn).
		AddItem(nil, 1, 0, false).
		AddItem(statusBar, 0, 1, false).
		AddItem(nil, 2, 0, false)
	statusFlex.SetBackgroundColor(bgInput)

	mainFlex := tview.NewFlex().
		SetDirection(tview.FlexColumn).
		AddItem(chatView, 0, 1, false)

	root := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(mainFlex, 0, 1, false).
		AddItem(statusFlex, 1, 0, false).
		AddItem(inputFlex, 2, 0, true).
		AddItem(nil, 1, 0, false)

	inputField.SetDoneFunc(func(key tcell.Key) {
		if key != tcell.KeyEnter {
			return
		}
		input := strings.TrimSpace(inputField.GetText())
		if input == "" {
			return
		}

		inputField.SetText("")
		mu.Lock()
		history = append(history, turn{role: "user", content: input})
		historySnapshot := make([]turn, len(history))
		copy(historySnapshot, history)
		messages = append(messages, formatMessage("user", input))
		updateChat(chatView, messages)
		statusBar.SetText(fmt.Sprintf("  [%s]thinking...[-]", fgMuted.CSS()))
		mu.Unlock()

		go func(snapshot []turn) {
			refreshProject := func() {
				newEntries, err := prompt.BuildFileList(".")
				if err != nil {
					return
				}
				newPrompt := prompt.Build(newEntries)
				*systemPrompt = newPrompt
			}
			response, err := agentAsk(ctx, client, systemPrompt, snapshot, statusBar, app, refreshProject)

			app.QueueUpdateDraw(func() {
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					messages = append(messages, formatMessage("error", err.Error()))
					statusBar.SetText(fmt.Sprintf("  [%s]%s[-]", fgRed.CSS(), err.Error()))
				} else {
					history = append(history, turn{role: "assistant", content: response})
					formatted, codeBlock := formatAssistantMessage(response)
					if codeBlock != "" {
						lastCodeBlock = codeBlock
					}
					messages = append(messages, formatted)
					statusBar.SetText(statusDefault())
				}
				updateChat(chatView, messages)
			})
		}(historySnapshot)
	})

	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyCtrlC:
			cancel()
			app.Stop()
			return nil

		case tcell.KeyCtrlL:
			mu.Lock()
			messages = []string{}
			history = []turn{} // also clear history so model forgets too
			updateChat(chatView, messages)
			mu.Unlock()
			return nil

		case tcell.KeyCtrlB:
			statusBar.SetText(statusDefault())
			app.SetFocus(inputField)
			return nil

		case tcell.KeyCtrlY:
			if lastCodeBlock == "" {
				statusBar.SetText(fmt.Sprintf("  [%s]no code block to copy[-]", fgMuted.CSS()))
			} else {
				statusBar.SetText(CopyToClipboard(lastCodeBlock))
			}
			return nil

		case tcell.KeyEscape:
			app.SetFocus(inputField)
			return nil
		}
		return event
	})

	app.SetBeforeDrawFunc(func(screen tcell.Screen) bool {
		screen.Fill(' ', tcell.StyleDefault.Background(bgMain))
		return false
	})

	if err := app.SetRoot(root, true).SetFocus(inputField).EnableMouse(true).Run(); err != nil {
		log.Fatalf("error starting chat: %v", err)
	}
}
