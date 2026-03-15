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
	bgMain    = tcell.GetColor("#0f1115")
	bgInput   = tcell.GetColor("#14161b")
	bgSidebar = tcell.GetColor("#0d0f13")
	fgText    = tcell.ColorWhite
	fgMuted   = tcell.GetColor("#9aa0a6")
	fgGreen   = tcell.GetColor("#3bb88a")
	_         = fgMuted
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
		sb.WriteString("\n[#3bb88a]  ╔═ code ══════════════════════════════════[-]\n")
		for _, line := range lines {
			sb.WriteString(fmt.Sprintf("[#3bb88a]  ║[-] [#ffffff]%s[-]\n", line))
		}
		sb.WriteString("[#3bb88a]  ╚═ ctrl+y to copy ═══════════════════════[-]\n")
		return sb.String()
	})
	return result, lastCode
}

func formatMessage(role, text string) string {
	var roleTag string
	switch role {
	case "user":
		roleTag = "[#c792ea]> you[-]"
	case "assistant":
		roleTag = "[#3bb88a]> guild[-]"
	case "error":
		roleTag = "[#f07178]> error[-]"
	default:
		roleTag = fmt.Sprintf("[#9aa0a6]> %s[-]", role)
	}
	header := roleTag + "\n"
	body := "  " + strings.ReplaceAll(text, "\n", "\n  ") + "\n"
	divider := "[#1e2025]────────────────────────────────────────[-]\n"
	return header + body + divider
}

func formatAssistantMessage(text string) (string, string) {
	rendered, lastCode := renderCodeBlocks(text)
	header := "[#3bb88a]> guild[-]\n"
	body := "  " + strings.ReplaceAll(rendered, "\n", "\n  ") + "\n"
	divider := "[#1e2025]────────────────────────────────────────[-]\n"
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
	list.SetSelectedBackgroundColor(tcell.GetColor("#1e2530"))
	list.SetSelectedTextColor(fgGreen)
	list.SetTitle(" files ").SetTitleColor(fgGreen)
	list.SetBorder(true).SetBorderColor(tcell.GetColor("#1e2025"))
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
		`[#3bb88a]
  ██████╗ ██╗   ██╗██╗██╗     ██████╗
 ██╔════╝ ██║   ██║██║██║     ██╔══██╗
 ██║  ███╗██║   ██║██║██║     ██║  ██║
 ██║   ██║██║   ██║██║██║     ██║  ██║
 ╚██████╔╝╚██████╔╝██║███████╗██████╔╝
  ╚═════╝  ╚═════╝ ╚═╝╚══════╝╚═════╝`,
		fmt.Sprintf("[#9aa0a6] \n Loaded %d project files into context.\n Type a message and press Enter.\n[-]\n", len(entries)),
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
		statusBar.SetText("  [#9aa0a6]thinking...[-]")
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
					statusBar.SetText("  [#f07178]" + err.Error() + "[-]")
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
				statusBar.SetText("  [#9aa0a6]no code block to copy[-]")
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
