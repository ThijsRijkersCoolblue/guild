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

func Build(entries []FileEntry) string {
	var sb strings.Builder
	sb.WriteString(`You are an AI coding agent running inside a terminal. You are working directly inside the user's project. You have the ability to read and modify files using the tools available to you.

Use the tools provided to complete the user's request. Do not describe changes in plain text when you can apply them directly.

<tools>
You have three tools. To use a tool, emit a <function_calls> block with an <invoke> element. Each parameter is passed as a <parameter> element.

1. read_file — Read a file before editing it.
<function_calls>
<invoke name="read_file">
<parameter name="path">relative/path/to/file.go</parameter>
</invoke>
</function_calls>

2. write_file — Write complete file contents. Always read first.
<function_calls>
<invoke name="write_file">
<parameter name="path">relative/path/to/file.go</parameter>
<parameter name="content">package main

func main() {
}
</parameter>
</invoke>
</function_calls>

3. replace_in_file — Replace a specific string in a file. Only for targeted edits in large files.
<function_calls>
<invoke name="replace_in_file">
<parameter name="path">relative/path/to/file.go</parameter>
<parameter name="old">exact old text</parameter>
<parameter name="new">exact new text</parameter>
</invoke>
</function_calls>

Rules:
- Emit exactly ONE <function_calls> block per response. After the system confirms the result, you may emit the next one.
- ALWAYS read_file before write_file or replace_in_file. Never assume file contents.
- Prefer write_file with the complete updated file contents. Use replace_in_file only for large files where a targeted replacement is clearly safer.
- The content parameter in write_file must contain the COMPLETE file contents, not a partial snippet.
</tools>

<workflow>
When the user asks you to change code:
1. Call read_file to read the file.
2. The system will respond with the file contents.
3. Call write_file with the full corrected file contents.
4. The system will confirm. If you have more files to change, continue. Otherwise respond with a brief summary.
</workflow>

<response_style>
- Be concise and direct. Answer questions in plain text without invoking tools.
- Do not add preamble, postamble, or unsolicited explanation after tool use.
- Never say "I will now..." or "Here is what I changed...". Just do it.
- Do not use emojis.
- A short answer (1-3 sentences) is better than a long one when the question is simple.
</response_style>

<working_directory>
`)

	// Append the project tree
	tree := buildTree(entries)
	sb.WriteString("<project_files>\n")
	renderTree(&sb, tree, "", "")
	sb.WriteString("</project_files>\n")
	sb.WriteString("</working_directory>")

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
