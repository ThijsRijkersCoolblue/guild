package tui

import (
	"context"
	"fmt"
	"guild/llm"
	"guild/prompt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

type ProgressKind string

const (
	ProgressThinking  ProgressKind = "thinking"
	ProgressSearching ProgressKind = "searching"
	ProgressReading   ProgressKind = "reading"
	ProgressWriting   ProgressKind = "writing"
	ProgressUpdating  ProgressKind = "updating"
	ProgressDone      ProgressKind = "done"
	ProgressError     ProgressKind = "error"
)

type ProgressEvent struct {
	Kind    ProgressKind
	Detail  string
	Attempt int
}

const fileContentMarker = "system: Contents of "

func globMatches(relPath, pattern string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" || pattern == "**/*" {
		return true
	}
	relPath = filepath.ToSlash(relPath)

	if strings.HasPrefix(pattern, "**/") {
		suffixPattern := strings.TrimPrefix(pattern, "**/")
		if strings.Contains(suffixPattern, "/") {
			ok, _ := path.Match(suffixPattern, relPath)
			return ok
		}
		ok, _ := path.Match(suffixPattern, path.Base(relPath))
		return ok
	}

	ok, _ := path.Match(pattern, relPath)
	if ok {
		return true
	}
	if !strings.Contains(pattern, "/") {
		ok, _ = path.Match(pattern, path.Base(relPath))
		return ok
	}
	return false
}

func evictFileContent(conversation, path string) string {
	startMarker := fmt.Sprintf("system: Contents of %s:\n```\n", path)
	startIdx := strings.Index(conversation, startMarker)
	if startIdx == -1 {
		return conversation
	}
	endMarker := "\n```\n"
	endIdx := strings.Index(conversation[startIdx+len(startMarker):], endMarker)
	if endIdx == -1 {
		return conversation
	}
	endIdx += startIdx + len(startMarker) + len(endMarker)

	replacement := fmt.Sprintf("system: [contents of %s loaded and applied]\n", path)
	return conversation[:startIdx] + replacement + conversation[endIdx:]
}

func agentAsk(
	ctx context.Context,
	client llm.LLM,
	systemPrompt *string,
	history []turn,
	onFileWritten func(),
	onFileContext func(string),
	onProgress func(ProgressEvent),
) (string, error) {
	emit := func(kind ProgressKind, detail string, attempt int) {
		if onProgress != nil {
			onProgress(ProgressEvent{Kind: kind, Detail: detail, Attempt: attempt})
		}
	}

	// actionLog tracks a short human-readable summary of completed operations.
	// It is appended to the final response so the caller sees what was done.
	var actionLog []string

	// conversation is built once from history and extended incrementally.
	// We never rebuild it from scratch after a successful write, instead we
	// append a compact tail and evict stale file content, keeping the window small.
	conversation := historyToPrompt(*systemPrompt, history)

	for attempt := range 10 {
		n := attempt + 1

		emit(ProgressThinking, "calling model…", n)

		response, err := client.Ask(ctx, conversation)
		if err != nil {
			emit(ProgressError, err.Error(), n)
			return "", err
		}

		// Emit a short reasoning preview (first 120 chars of stripped text).
		preview := strings.TrimSpace(StripActions(response))
		if len(preview) > 120 {
			preview = preview[:120] + "…"
		}
		if preview != "" {
			emit(ProgressThinking, preview, n)
		}

		a := ParseAction(response)
		if a == nil {
			finalText := StripActions(response)
			if len(actionLog) > 0 {
				finalText = strings.Join(actionLog, "\n") + "\n\n" + finalText
			}
			emit(ProgressDone, "", n)
			return strings.TrimSpace(finalText), nil
		}

		text := StripActions(response)

		switch a.Type {
		case "glob_files":
			emit(ProgressSearching, fmt.Sprintf("glob %s", a.Pattern), n)
			entries, err := prompt.BuildFileList(".")
			if err != nil {
				emit(ProgressError, fmt.Sprintf("glob prep failed: %v", err), n)
				conversation += fmt.Sprintf(
					"assistant: %s\n\nsystem: glob_files failed to list files: %v\n\n",
					text, err,
				)
				break
			}

			pattern := strings.TrimSpace(a.Pattern)
			if pattern == "" {
				conversation += fmt.Sprintf(
					"assistant: %s\n\nsystem: glob_files requires a non-empty pattern.\n\n",
					text,
				)
				break
			}

			var matches []string
			for _, e := range entries {
				relPath := filepath.ToSlash(e.RelPath)
				if globMatches(relPath, pattern) {
					matches = append(matches, relPath)
				}
			}

			if len(matches) == 0 {
				conversation += fmt.Sprintf(
					"assistant: %s\n\nsystem: glob_files found no files for pattern %q.\n\n",
					text, pattern,
				)
			} else {
				conversation += fmt.Sprintf(
					"assistant: %s\n\nsystem: glob_files results for pattern %q:\n%s\n\n",
					text, pattern, strings.Join(matches, "\n"),
				)
			}

		case "grep_files":
			emit(ProgressSearching, fmt.Sprintf("grep %s", a.Pattern), n)
			entries, err := prompt.BuildFileList(".")
			if err != nil {
				emit(ProgressError, fmt.Sprintf("search prep failed: %v", err), n)
				conversation += fmt.Sprintf(
					"assistant: %s\n\nsystem: grep_files failed to list files: %v\n\n",
					text, err,
				)
				break
			}

			re, err := regexp.Compile(a.Pattern)
			if err != nil {
				emit(ProgressError, fmt.Sprintf("invalid regex: %v", err), n)
				conversation += fmt.Sprintf(
					"assistant: %s\n\nsystem: grep_files invalid regex %q: %v\n\n",
					text, a.Pattern, err,
				)
				break
			}

			globPattern := strings.TrimSpace(a.Glob)
			if globPattern == "" {
				globPattern = "**/*"
			}

			var lines []string
			for _, e := range entries {
				relPath := filepath.ToSlash(e.RelPath)
				if !globMatches(relPath, globPattern) {
					continue
				}
				bytes, err := ReadFileOS(e.Path)
				if err != nil {
					continue
				}
				for idx, line := range strings.Split(string(bytes), "\n") {
					if re.MatchString(line) {
						lines = append(lines, fmt.Sprintf("%s:%d: %s", relPath, idx+1, line))
					}
				}
			}

			if len(lines) == 0 {
				conversation += fmt.Sprintf(
					"assistant: %s\n\nsystem: grep_files found no matches for regex %q in %q.\n\n",
					text, a.Pattern, globPattern,
				)
			} else {
				conversation += fmt.Sprintf(
					"assistant: %s\n\nsystem: grep_files matches for regex %q in %q:\n%s\n\n",
					text, a.Pattern, globPattern, strings.Join(lines, "\n"),
				)
			}

		case "read_file":
			if filepath.IsAbs(a.Path) {
				emit(ProgressError, fmt.Sprintf("absolute path rejected: %s", a.Path), n)
				conversation += fmt.Sprintf(
					"assistant: %s\n\nsystem: read_file requires a workspace-relative path, got absolute path %q. Use glob_files first.\n\n",
					text, a.Path,
				)
				break
			}
			emit(ProgressReading, a.Path, n)
			fileContent, err := prompt.ReadFile(a.Path)
			if err != nil {
				emit(ProgressError, fmt.Sprintf("read %s: %v", a.Path, err), n)
				conversation += fmt.Sprintf(
					"assistant: %s\n\nsystem: Could not read %s: %v. Try a different path.\n\n",
					text, a.Path, err,
				)
			} else {
				if onFileContext != nil {
					onFileContext(a.Path)
				}
				conversation += fmt.Sprintf(
					"assistant: %s\n\nsystem: Contents of %s:\n```\n%s\n```\nNow apply the change using write_file.\n\n",
					text, a.Path, fileContent,
				)
			}

		case "write_file":
			if filepath.IsAbs(a.Path) {
				emit(ProgressError, fmt.Sprintf("absolute path rejected: %s", a.Path), n)
				conversation += fmt.Sprintf(
					"assistant: %s\n\nsystem: write_file requires a workspace-relative path, got absolute path %q. Use glob_files first.\n\n",
					text, a.Path,
				)
				break
			}
			parent := filepath.Dir(a.Path)
			if parent != "." {
				if st, err := os.Stat(parent); err != nil || !st.IsDir() {
					emit(ProgressError, fmt.Sprintf("parent dir missing for %s", a.Path), n)
					conversation += fmt.Sprintf(
						"assistant: %s\n\nsystem: write_file failed because parent directory %q does not exist. Use glob_files to find the correct path, or create files only in existing directories.\n\n",
						text, parent,
					)
					break
				}
			}
			emit(ProgressWriting, a.Path, n)
			if err := WriteFile(a.Path, a.Content); err != nil {
				emit(ProgressError, fmt.Sprintf("write %s: %v", a.Path, err), n)
				conversation += fmt.Sprintf(
					"assistant: %s\n\nsystem: write_file failed: %v. Try again.\n\n",
					text, err,
				)
			} else {
				if onFileContext != nil {
					onFileContext(a.Path)
				}
				actionLog = append(actionLog, fmt.Sprintf("written to %s", a.Path))
				onFileWritten()

				// Evict the now stale file contents from the conversation so they
				// don't keep consuming tokens on subsequent iterations.
				conversation = evictFileContent(conversation, a.Path)

				// Append a compact tail, do NOT rebuild from full history.
				conversation += fmt.Sprintf(
					"assistant: %s\n\nsystem: Successfully written to %s. If you have more files to change, do them now. Otherwise respond with a brief summary.\n\n",
					text, a.Path,
				)
			}

		case "replace_in_file":
			if filepath.IsAbs(a.Path) {
				emit(ProgressError, fmt.Sprintf("absolute path rejected: %s", a.Path), n)
				conversation += fmt.Sprintf(
					"assistant: %s\n\nsystem: replace_in_file requires a workspace-relative path, got absolute path %q. Use glob_files first.\n\n",
					text, a.Path,
				)
				break
			}
			emit(ProgressUpdating, a.Path, n)
			existing, err := prompt.ReadFile(a.Path)
			if err != nil {
				emit(ProgressError, fmt.Sprintf("read %s: %v", a.Path, err), n)
				conversation += fmt.Sprintf(
					"assistant: %s\n\nsystem: Could not read %s: %v\n\n",
					text, a.Path, err,
				)
			} else if !strings.Contains(existing, a.Old) {
				emit(ProgressError, fmt.Sprintf("old string not found in %s", a.Path), n)
				conversation += fmt.Sprintf(
					"assistant: %s\n\nsystem: replace_in_file failed — exact \"old\" string not found in %s. Use write_file with the full corrected content instead.\n\nCurrent file:\n```\n%s\n```\n\n",
					text, a.Path, existing,
				)
			} else if err := ReplaceInFile(a.Path, a.Old, a.New); err != nil {
				emit(ProgressError, fmt.Sprintf("replace %s: %v", a.Path, err), n)
				conversation += fmt.Sprintf(
					"assistant: %s\n\nsystem: replace_in_file failed: %v\n\n",
					text, err,
				)
			} else {
				if onFileContext != nil {
					onFileContext(a.Path)
				}
				actionLog = append(actionLog, fmt.Sprintf("updated %s", a.Path))
				onFileWritten()

				// Evict the stale file contents loaded for this replace operation.
				conversation = evictFileContent(conversation, a.Path)

				// Append a compact tail, do NOT rebuild from full history.
				conversation += fmt.Sprintf(
					"assistant: %s\n\nsystem: Successfully updated %s. If you have more files to change, do them now. Otherwise respond with a brief summary.\n\n",
					text, a.Path,
				)
			}

		default:
			emit(ProgressDone, "", n)
			return StripActions(response), nil
		}
	}

	emit(ProgressError, "exceeded maximum attempts", 10)
	return "", fmt.Errorf("could not complete the change after multiple attempts")
}
