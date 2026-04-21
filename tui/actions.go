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

// FunctionCallRegex matches the standard <function_calls>...<invoke>...</invoke>...</function_calls> format.
var FunctionCallRegex = regexp.MustCompile(`(?s)<function_calls>\s*(.*?)</function_calls>`)
var InvokeRegex = regexp.MustCompile(`(?s)<invoke\s+name="([^"]+)">\s*(.*?)</invoke>`)
var ParamRegex = regexp.MustCompile(`(?s)<parameter\s+name="([^"]+)">(.*?)</parameter>`)

// Legacy ActionRegex kept for backward compatibility.
var ActionRegex = regexp.MustCompile("(?s)`*<action>(.*?)</action>`*")
var CodeBlockRegex = regexp.MustCompile("(?s)```(?:[a-zA-Z]*)\n(.*?)```")

type action struct {
	Type    string
	Path    string
	Content string
	Old     string
	New     string
	Pattern string
	Glob    string
}

func ParseAction(response string) *action {
	// Try the standard <function_calls> format first.
	if a := parseFunctionCall(response); a != nil {
		return a
	}
	// Fall back to legacy <action>{JSON}</action> format.
	return parseLegacyAction(response)
}

// parseFunctionCall parses <function_calls><invoke name="..."><parameter name="...">...</parameter></invoke></function_calls>
func parseFunctionCall(response string) *action {
	fcMatch := FunctionCallRegex.FindStringSubmatch(response)
	if fcMatch == nil {
		return nil
	}

	invokeMatch := InvokeRegex.FindStringSubmatch(fcMatch[1])
	if invokeMatch == nil {
		return nil
	}

	toolName := invokeMatch[1]
	paramsBlock := invokeMatch[2]

	params := make(map[string]string)
	paramMatches := ParamRegex.FindAllStringSubmatch(paramsBlock, -1)
	for _, m := range paramMatches {
		params[m[1]] = m[2]
	}

	a := &action{Type: toolName}

	switch toolName {
	case "glob_files":
		a.Pattern = params["pattern"]
		if a.Pattern == "" {
			return nil
		}
	case "grep_files":
		a.Pattern = params["pattern"]
		a.Glob = params["glob"]
		if a.Pattern == "" {
			return nil
		}
	case "read_file":
		a.Path = params["path"]
		if a.Path == "" {
			return nil
		}
	case "write_file":
		a.Path = params["path"]
		a.Content = params["content"]
		if a.Path == "" {
			return nil
		}
	case "replace_in_file":
		a.Path = params["path"]
		a.Old = params["old"]
		a.New = params["new"]
		if a.Path == "" || a.Old == "" {
			return nil
		}
	default:
		return nil
	}

	return a
}

// parseLegacyAction parses the old <action>{"type":"...", ...}</action> JSON format.
func parseLegacyAction(response string) *action {
	matches := ActionRegex.FindStringSubmatch(response)
	if matches == nil {
		return nil
	}

	jsonStr := strings.TrimSpace(matches[1])
	jsonStr = repairJSON(jsonStr)

	// Use a temporary struct for JSON unmarshaling since the main action struct
	// no longer has json tags (fields are populated directly from XML params).
	var parsed struct {
		Type    string `json:"type"`
		Path    string `json:"path"`
		Content string `json:"content"`
		Old     string `json:"old"`
		New     string `json:"new"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		return nil
	}
	if parsed.Type == "" {
		return nil
	}
	return &action{
		Type:    parsed.Type,
		Path:    parsed.Path,
		Content: parsed.Content,
		Old:     parsed.Old,
		New:     parsed.New,
	}
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
			return fmt.Sprintf("  [%s]copied to clipboard![-]", fgGreen.CSS())
		}
	}

	path := CodeCopyPath()
	_ = os.WriteFile(path, []byte(text), 0644)
	return fmt.Sprintf("  [%s]clipboard unavailable — saved to %s[-]", fgYellow.CSS(), path)
}

// StripActions removes both <function_calls>...</function_calls> and legacy <action>...</action> blocks from the response.
func StripActions(response string) string {
	s := FunctionCallRegex.ReplaceAllString(response, "")
	s = ActionRegex.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}
