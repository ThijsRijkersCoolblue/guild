package prompt

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

var ignoredDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true,
	"dist": true, "build": true, ".idea": true, ".vscode": true,
}

var ignoredExts = map[string]bool{
	".exe": true, ".bin": true, ".png": true, ".jpg": true,
	".jpeg": true, ".gif": true, ".zip": true, ".tar": true,
	".gz": true, ".sum": true, ".lock": true,
}

type FileEntry struct {
	Path    string
	RelPath string
}

func BuildFileList(root string) ([]FileEntry, error) {
	var entries []FileEntry

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if ignoredDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if ignoredExts[ext] {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			rel = path
		}

		entries = append(entries, FileEntry{Path: path, RelPath: rel})
		return nil
	})

	return entries, err
}

func Build(entries []FileEntry) string {
	var sb strings.Builder

	sb.WriteString(`You are an AI coding assistant running inside a terminal.
You are working directly inside the user's project. You have the ability to read and modify files.

CRITICAL RULES - YOU MUST FOLLOW THESE WITHOUT EXCEPTION:
1. When the user asks you to change, edit, fix, update, or modify a file — you MUST emit an action to do it. Never just show code in markdown and tell the user to apply it manually.
2. When you do not yet have the file contents, FIRST emit a read_file action, then after receiving the contents emit the edit action.
3. NEVER say "here is what you should change" or "replace X with Y" in plain text. Always use the action system to apply changes directly.
4. You are an agent that acts — not an assistant that gives instructions.

AVAILABLE ACTIONS:

Read a file (always do this first before editing):
<action>{"type": "read_file", "path": "relative/path/to/file.go"}</action>

Write an entire file (PREFERRED way to apply edits — always use this after reading):
<action>{"type": "write_file", "path": "relative/path/to/file.go", "content": "full file content here"}</action>

Apply a targeted edit (only use if the file is very large and you are 100% certain of the exact string):
<action>{"type": "replace_in_file", "path": "relative/path/to/file.go", "old": "exact existing code", "new": "replacement code"}</action>

WORKFLOW:
1. User asks to edit a file → emit read_file to get current contents
2. After receiving contents → emit write_file with the complete updated file
3. Never skip the read_file step — always read before writing
4. Briefly explain what you changed AFTER the action, not before
5. Do not ask for confirmation — just do it
6. NEVER wrap <action> tags inside markdown code blocks or backticks — always write them as plain raw text
7. NEVER use emojis in your responses
8. ONLY create or modify files when the user explicitly asks you to. If the user asks for an example, explanation, or demo — respond with a code block in markdown, do NOT emit a write_file action

PROJECT FILES:
`)

	for _, e := range entries {
		sb.WriteString(fmt.Sprintf("  %s\n", e.RelPath))
	}

	return sb.String()
}

func ReadFile(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(content), nil
}
