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
	bgMain     = tcell.GetColor("#191724")
	bgInput    = tcell.GetColor("#1f1d2e")
	fgText     = tcell.GetColor("#e0def4")
	fgMuted    = tcell.GetColor("#6e6a86")
	fgGreen    = tcell.GetColor("#31748f")
	fgRed      = tcell.GetColor("#eb6f92")
	fgBlue     = tcell.GetColor("#9ccfd8")
	bgBorder   = tcell.GetColor("#403d52")
	fgCode     = tcell.GetColor("#e0def4")
	fgOrange   = tcell.GetColor("#f6c177")
	fgYellow   = tcell.GetColor("#f6c177")
	bgProgress = tcell.GetColor("#13111e")
)

type turn struct {
	role    string
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
		sb.WriteString(fmt.Sprintf("\n[%s]  ╭─ code[-]\n", fgBlue.CSS()))
		for _, line := range lines {
			sb.WriteString(fmt.Sprintf("[%s]  │[-] [%s]%s[-]\n", fgBlue.CSS(), fgCode.CSS(), line))
		}
		sb.WriteString(fmt.Sprintf("[%s]  ╰─ ctrl+y copies this block[-]\n", fgBlue.CSS()))
		return sb.String()
	})
	return result, lastCode
}

func formatMessage(role, text string) string {
	var roleTag string
	switch role {
	case "user":
		roleTag = fmt.Sprintf("[%s]you[-]", fgBlue.CSS())
	case "assistant":
		roleTag = fmt.Sprintf("[%s]assistent[-]", fgGreen.CSS())
	case "error":
		roleTag = fmt.Sprintf("[%s]error[-]", fgRed.CSS())
	default:
		roleTag = fmt.Sprintf("[%s]%s[-]", fgMuted.CSS(), role)
	}
	header := fmt.Sprintf("%s [%s]•[-]\n", roleTag, bgBorder.CSS())
	body := "  " + strings.ReplaceAll(text, "\n", "\n  ") + "\n"
	divider := fmt.Sprintf("[%s]────────────────────────────────────────[-]\n", bgBorder.CSS())
	return header + body + divider
}

func formatAssistantMessage(text string) (string, string) {
	rendered, lastCode := renderCodeBlocks(text)
	header := fmt.Sprintf("[%s]assistent[-] [%s]•[-]\n", fgGreen.CSS(), bgBorder.CSS())
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
	return fmt.Sprintf("[%s]model:[-] [%s]%s[-]", fgMuted.CSS(), fgBlue.CSS(), model)
}

const statusDefaultFmt = "  [#6e6a86]ctrl+c[-] quit   [#6e6a86]ctrl+l[-] clear   [#6e6a86]ctrl+y[-] copy code   [#6e6a86]ctrl+r[-] reasoning"

func statusDefault() string {
	return statusDefaultFmt + "   [#e0def4]│[-]   " + modelindicator()
}

type progressPanel struct {
	mu      sync.Mutex
	view    *tview.TextView
	events  []ProgressEvent
	visible bool
}

func newProgressPanel(app *tview.Application) *progressPanel {
	view := tview.NewTextView()
	view.SetDynamicColors(true)
	view.SetScrollable(false)
	view.SetWrap(false)
	view.SetBackgroundColor(bgProgress)
	view.SetChangedFunc(func() { app.Draw() })
	return &progressPanel{view: view}
}

func kindIcon(k ProgressKind) string {
	switch k {
	case ProgressThinking:
		return fmt.Sprintf("[%s]◆ thinking[-]", fgYellow.CSS())
	case ProgressReading:
		return fmt.Sprintf("[%s]↓ read[-]", fgGreen.CSS())
	case ProgressWriting:
		return fmt.Sprintf("[%s]↑ write[-]", fgBlue.CSS())
	case ProgressUpdating:
		return fmt.Sprintf("[%s]± patch[-]", fgOrange.CSS())
	case ProgressDone:
		return fmt.Sprintf("[%s]✓ done[-]", fgGreen.CSS())
	case ProgressError:
		return fmt.Sprintf("[%s]✗ error[-]", fgRed.CSS())
	default:
		return fmt.Sprintf("[%s]· …[-]", fgMuted.CSS())
	}
}

func (p *progressPanel) push(ev ProgressEvent, app *tview.Application) {
	p.mu.Lock()
	p.events = append(p.events, ev)
	evsCopy := make([]ProgressEvent, len(p.events))
	copy(evsCopy, p.events)
	p.mu.Unlock()

	app.QueueUpdateDraw(func() {
		p.render(evsCopy)
	})
}

func (p *progressPanel) render(evs []ProgressEvent) {
	p.view.Clear()

	// Header label, no fixed width spanning, avoids overflow on narrow terminals.
	fmt.Fprintf(p.view, "[%s]  reasoning trace[-]\n", fgMuted.CSS())

	// Show last N events so the panel stays compact
	const maxShow = 6
	start := 0
	if len(evs) > maxShow {
		start = len(evs) - maxShow
		fmt.Fprintf(p.view, "[%s]  … %d earlier steps hidden …[-]\n", fgMuted.CSS(), start)
	}

	for i, ev := range evs[start:] {
		isLast := (start + i) == len(evs)-1
		prefix := "  "
		if isLast {
			prefix = fmt.Sprintf("[%s]▶[-] ", fgYellow.CSS())
		}

		icon := kindIcon(ev.Kind)

		if ev.Detail != "" {
			fmt.Fprintf(p.view, "%s%s  [%s]%s[-]\n", prefix, icon, fgMuted.CSS(), ev.Detail)
		} else {
			fmt.Fprintf(p.view, "%s%s\n", prefix, icon)
		}
	}
}

func (p *progressPanel) clear() {
	p.mu.Lock()
	p.events = nil
	p.mu.Unlock()
	p.view.Clear()
}

func (p *progressPanel) toggle() bool {
	p.visible = !p.visible
	return p.visible
}

func StartChat(parentCtx context.Context, client llm.LLM) {
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

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

	// ── chat view ────────────────────────────────────────────────────────────
	chatView := tview.NewTextView()
	chatView.
		SetDynamicColors(true).
		SetScrollable(true).
		SetWrap(true).
		SetWordWrap(true)
	chatView.SetBackgroundColor(bgMain)
	chatView.SetTextColor(fgText)
	chatView.SetChangedFunc(func() { app.Draw() })

	// ── progress / reasoning panel ───────────────────────────────────────────
	pp := newProgressPanel(app)

	// ── input field ──────────────────────────────────────────────────────────
	inputField := tview.NewInputField().
		SetLabel("  > ").
		SetFieldWidth(0).
		SetFieldBackgroundColor(bgInput).
		SetFieldTextColor(fgText)
	inputField.SetLabelColor(fgBlue)
	inputField.SetLabelStyle(tcell.StyleDefault.Foreground(fgBlue).Background(bgInput))
	inputField.SetBackgroundColor(bgInput)

	// ── status bar ───────────────────────────────────────────────────────────
	statusBar := tview.NewTextView()
	statusBar.SetDynamicColors(true)
	statusBar.SetBackgroundColor(bgInput)
	statusBar.SetText(statusDefault())

	// ── initial welcome messages ─────────────────────────────────────────────
	messages := []string{
		fmt.Sprintf("[%s::b]\n%s[-]", fgBlue.CSS(), ` ██████╗ ██╗   ██╗██╗██╗     ██████╗
██╔════╝ ██║   ██║██║██║     ██╔══██╗
██║  ███╗██║   ██║██║██║     ██║  ██║
██║   ██║██║   ██║██║██║     ██║  ██║
╚██████╔╝╚██████╔╝██║███████╗██████╔╝
 ╚═════╝  ╚═════╝ ╚═╝╚══════╝╚═════╝`),
		fmt.Sprintf("[%s]\n Connected to workspace with %d files in context.\n Ask anything and press Enter.\n[-]\n", fgMuted.CSS(), len(entries)),
	}
	updateChat(chatView, messages)

	var history []turn
	var mu sync.Mutex
	var lastCodeBlock string

	// ── layout helpers ───────────────────────────────────────────────────────
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
		AddItem(nil, 2, 0, false).
		AddItem(chatView, 0, 1, false)

	const progressHeight = 10
	progressFlex := tview.NewFlex().
		SetDirection(tview.FlexColumn).
		AddItem(nil, 1, 0, false).
		AddItem(pp.view, 0, 1, false).
		AddItem(nil, 2, 0, false)
	progressFlex.SetBackgroundColor(bgProgress)

	root := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(mainFlex, 0, 1, false).
		AddItem(progressFlex, 0, 0, false).
		AddItem(statusFlex, 1, 0, false).
		AddItem(inputFlex, 2, 0, true).
		AddItem(nil, 1, 0, false)

	showProgress := func(visible bool) {
		if visible {
			root.ResizeItem(progressFlex, progressHeight, 0)
		} else {
			root.ResizeItem(progressFlex, 0, 0)
		}
	}

	// ── input handler ────────────────────────────────────────────────────────
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
		statusBar.SetText(fmt.Sprintf("  [%s]thinking...[-]", fgBlue.CSS()))
		mu.Unlock()

		pp.clear()
		pp.visible = true
		showProgress(true)

		go func(snapshot []turn) {
			refreshProject := func() {
				newEntries, err := prompt.BuildFileList(".")
				if err != nil {
					return
				}
				newPrompt := prompt.Build(newEntries)
				*systemPrompt = newPrompt
			}

			onProgress := func(ev ProgressEvent) {
				pp.push(ev, app)
				app.QueueUpdateDraw(func() {
					switch ev.Kind {
					case ProgressThinking:
						statusBar.SetText(fmt.Sprintf("  [#f6c177]thinking...[-]"))
					case ProgressReading:
						statusBar.SetText(fmt.Sprintf("  [#f6c177]reading %s...[-]", ev.Detail))
					case ProgressWriting:
						statusBar.SetText(fmt.Sprintf("  [#f6c177]writing %s...[-]", ev.Detail))
					case ProgressUpdating:
						statusBar.SetText(fmt.Sprintf("  [#f6c177]updating %s...[-]", ev.Detail))
					}
				})
			}

			response, err := agentAsk(ctx, client, systemPrompt, snapshot, refreshProject, onProgress)

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

	// ── global key capture ────────────────────────────────────────────────────
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyCtrlC:
			cancel()
			app.Stop()
			return nil

		case tcell.KeyCtrlL:
			mu.Lock()
			messages = []string{}
			history = []turn{}
			updateChat(chatView, messages)
			mu.Unlock()
			pp.clear()
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

		case tcell.KeyCtrlR:
			visible := pp.toggle()
			showProgress(visible)
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
