package tui

import (
	"context"
	"fmt"
	"guild/llm"
	"guild/prompt"
	"log"
	"strings"
	"sync"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

var (
	bgMain      = tcell.GetColor("#0f1115")
	bgInput     = tcell.GetColor("#14161b")
	bgSidebar   = tcell.GetColor("#0d0f13")
	fgText      = tcell.ColorWhite
	fgMuted     = tcell.GetColor("#9aa0a6")
	fgGreen     = tcell.GetColor("#3bb88a")
	fgRed       = tcell.GetColor("#f07178")
	fgPurple    = tcell.GetColor("#c792ea")
	bgBorder    = tcell.GetColor("#1e2025")
	bgHighlight = tcell.GetColor("#1e2530")
	fgCode      = tcell.GetColor("#ffffff")
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
		roleTag = fmt.Sprintf("[%s]> guild[-]", fgGreen.CSS())
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
	header := fmt.Sprintf("[%s]> guild[-]\n", fgGreen.CSS())
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

func buildSidebar(entries []prompt.FileEntry, onSelect func(string)) *tview.List {
	list := tview.NewList()
	list.SetBackgroundColor(bgSidebar)
	list.SetMainTextColor(fgText)
	list.SetSelectedBackgroundColor(bgHighlight)
	list.SetSelectedTextColor(fgGreen)
	list.SetTitle(" files ").SetTitleColor(fgGreen)
	list.SetBorder(true).SetBorderColor(bgBorder)
	list.ShowSecondaryText(false)

	for _, e := range entries {
		path := e.RelPath
		list.AddItem(path, "", 0, func() {
			onSelect(path)
		})
	}

	return list
}

const statusDefault = "  [#9aa0a6]ctrl+c[-] quit   [#9aa0a6]ctrl+l[-] clear   [#9aa0a6]ctrl+b[-] files   [#9aa0a6]ctrl+y[-] copy code   [#9aa0a6]enter[-] send"
const statusSidebar = "  [#9aa0a6]ctrl+b[-] hide files   [#9aa0a6]ctrl+f[-] focus   [#9aa0a6]esc[-] back to input"

func StartChat(parentCtx context.Context, client llm.LLM) {
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

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
	statusBar.SetText(statusDefault)

	sidebar := buildSidebar(entries, func(path string) {
		current := inputField.GetText()
		if current == "" {
			inputField.SetText("explain " + path)
		} else {
			inputField.SetText(current + " " + path)
		}
		app.SetFocus(inputField)
	})

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

	mainFlex := tview.NewFlex().
		SetDirection(tview.FlexColumn).
		AddItem(sidebar, 0, 0, false).
		AddItem(chatView, 0, 1, false)

	root := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(mainFlex, 0, 1, false).
		AddItem(statusBar, 1, 0, false).
		AddItem(inputFlex, 2, 0, true).
		AddItem(nil, 1, 0, false)

	sidebarVisible := false

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
				app.QueueUpdateDraw(func() {
					sidebar.Clear()
					for _, e := range newEntries {
						path := e.RelPath
						sidebar.AddItem(path, "", 0, func() {
							current := inputField.GetText()
							if current == "" {
								inputField.SetText("explain " + path)
							} else {
								inputField.SetText(current + " " + path)
							}
							app.SetFocus(inputField)
						})
					}
				})
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
					statusBar.SetText(statusDefault)
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
			sidebarVisible = !sidebarVisible
			if sidebarVisible {
				mainFlex.ResizeItem(sidebar, 28, 0)
				statusBar.SetText(statusSidebar)
			} else {
				mainFlex.ResizeItem(sidebar, 0, 0)
				statusBar.SetText(statusDefault)
				app.SetFocus(inputField)
			}
			return nil

		case tcell.KeyCtrlF:
			if !sidebarVisible {
				sidebarVisible = true
				mainFlex.ResizeItem(sidebar, 28, 0)
				statusBar.SetText(statusSidebar)
			}
			app.SetFocus(sidebar)
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
