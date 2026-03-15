package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

var ActionRegex = regexp.MustCompile(`(?s)<action>(.*?)</action>`)
var ActionRegexUnclosed = regexp.MustCompile(`(?s)<action>(.*?)$`)
var CodeBlockRegex = regexp.MustCompile("(?s)```(?:[a-zA-Z]*)\n(.*?)```")

type action struct {
	Type    string `json:"type"`
	Path    string `json:"path"`
	Content string `json:"content"`
	Old     string `json:"old"`
	New     string `json:"new"`
}

func ParseAction(response string) *action {
	cleaned := strings.ReplaceAll(response, "`", "")

	matches := ActionRegex.FindStringSubmatch(cleaned)
	if matches == nil {
		matches = ActionRegexUnclosed.FindStringSubmatch(cleaned)
	}
	if matches == nil {
		return nil
	}
	jsonStr := strings.TrimSpace(matches[1])

	jsonStr = repairJSON(jsonStr)

	var a action
	if err := json.Unmarshal([]byte(jsonStr), &a); err != nil {
		return nil
	}
	if a.Type == "" {
		return nil
	}
	return &a
}

func repairJSON(s string) string {
	open := strings.Count(s, "{")
	close := strings.Count(s, "}")
	for i := 0; i < open-close; i++ {
		s += "}"
	}
	return s
}

func CodeCopyPath() string {
	return filepath.Join(os.TempDir(), "guild_copy.txt")
}

func CopyToClipboard(text string) string {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbcopy")
	case "windows":
		cmd = exec.Command("clip")
	default:
		// Linux / WSL — try xclip, xsel, then wl-copy, then clip.exe (WSL)
		if _, err := exec.LookPath("clip.exe"); err == nil {
			cmd = exec.Command("clip.exe")
		} else if _, err := exec.LookPath("xclip"); err == nil {
			cmd = exec.Command("xclip", "-selection", "clipboard")
		} else if _, err := exec.LookPath("xsel"); err == nil {
			cmd = exec.Command("xsel", "--clipboard", "--input")
		} else if _, err := exec.LookPath("wl-copy"); err == nil {
			cmd = exec.Command("wl-copy")
		}
	}

	if cmd != nil {
		cmd.Stdin = strings.NewReader(text)
		if err := cmd.Run(); err == nil {
			return "  [#3bb88a]copied to clipboard![-]"
		}
	}

	path := CodeCopyPath()
	_ = os.WriteFile(path, []byte(text), 0644)
	return fmt.Sprintf("  [#ffcb6b]clipboard unavailable — saved to %s[-]", path)
}

func StripActions(response string) string {
	return strings.TrimSpace(ActionRegex.ReplaceAllString(response, ""))
}
