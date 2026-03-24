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

const maxFileBytes = 8000

type FileEntry struct {
	Path    string
	RelPath string
}

// treeNode is used to build a compact directory tree for the prompt.
type treeNode struct {
	children map[string]*treeNode
	isFile   bool
}

func newNode() *treeNode {
	return &treeNode{children: make(map[string]*treeNode)}
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

// buildTree converts a flat file list into a nested treeNode for compact rendering.
func buildTree(entries []FileEntry) *treeNode {
	root := newNode()
	for _, e := range entries {
		parts := strings.Split(filepath.ToSlash(e.RelPath), "/")
		cur := root
		for i, p := range parts {
			if _, ok := cur.children[p]; !ok {
				cur.children[p] = newNode()
			}
			if i == len(parts)-1 {
				cur.children[p].isFile = true
			}
			cur = cur.children[p]
		}
	}
	return root
}

// renderTree writes a compact indented tree into sb.
func renderTree(sb *strings.Builder, node *treeNode, prefix string, name string) {
	if name != "" {
		sb.WriteString(prefix + name)
		if !node.isFile {
			sb.WriteString("/")
		}
		sb.WriteString("\n")
		prefix += "  "
	}
	// Sort keys for deterministic output.
	keys := make([]string, 0, len(node.children))
	for k := range node.children {
		keys = append(keys, k)
	}
	// Simple insertion sort — file lists are small.
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	for _, k := range keys {
		renderTree(sb, node.children[k], prefix, k)
	}
}

// Build returns the static system prompt. Pass this as the `system` field to
// the LLM API (not injected into messages) so the provider can cache it
// between turns — Anthropic charges ~10% for cache hits.
func Build(entries []FileEntry) string {
	var sb strings.Builder
	sb.WriteString(`You are an AI coding assistant running inside a terminal.
You are working directly inside the user's project. You have the ability to read and modify files.

RESPONSE MODE - DETERMINE THIS FIRST:
- If the user is ASKING A QUESTION (where, how, why, what, which) — answer in plain text. Do NOT emit any action.
- If the user wants an EXAMPLE or EXPLANATION — respond with a markdown code block. Do NOT emit write_file.
- If the user wants to CHANGE, EDIT, FIX, or CREATE a file — use the action system below.

CRITICAL RULES - ONLY APPLY WHEN IN EDIT MODE:
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
0. Determine response mode first (see RESPONSE MODE above). Only proceed below if in edit mode.
1. User asks to edit a file → emit read_file to get current contents
2. After receiving contents → emit write_file with the complete updated file
3. Never skip the read_file step — always read before writing
4. Briefly explain what you changed AFTER the action, not before
5. Do not ask for confirmation — just do it
6. NEVER wrap <action> tags inside markdown code blocks or backticks — always write them as plain raw text
7. NEVER use emojis in your responses
8. NEVER create or modify files unless the user explicitly asks you to

PROJECT FILES:
`)
	tree := buildTree(entries)
	renderTree(&sb, tree, "", "")
	return sb.String()
}

// ReadFile reads a file, capping at maxFileBytes to avoid flooding the context window.
func ReadFile(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if len(content) > maxFileBytes {
		return fmt.Sprintf("%s\n\n[file truncated at %d bytes — ask to see more if needed]",
			string(content[:maxFileBytes]), maxFileBytes), nil
	}
	return string(content), nil
}
