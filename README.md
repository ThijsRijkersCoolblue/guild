```
  ██████╗ ██╗   ██╗██╗██╗     ██████╗
 ██╔════╝ ██║   ██║██║██║     ██╔══██╗
 ██║  ███╗██║   ██║██║██║     ██║  ██║
 ██║   ██║██║   ██║██║██║     ██║  ██║
 ╚██████╔╝╚██████╔╝██║███████╗██████╔╝
  ╚═════╝  ╚═════╝ ╚═╝╚══════╝╚═════╝
```
guild is an AI-powered terminal assistant designed to answer questions, interact through a terminal UI, and make code adjustments.

## Setup & installation

#### Requirements
 
- [Go](https://go.dev/dl/) 1.21+
- An LLM provider (see below), Ollama for free local usage, or a cloud API key

### 1. Set environment variables

The app reads configuration from environment variables to decide which LLM provider to use.

**Variable setup:**
```bash
export LLM_PROVIDER=ollama | claude | gemini| openai
export LLM_MODEL= 

#If you dont use an local modal
export ANTHROPIC_API_KEY=
export GEMINI_API_KEY=
export OPENAI_API_KEY=
```

### 2. Run the app

```bash
go run ./main.go
```

### 3. Install

Once the project successfully launched, you can execute:

```bash
go install
```

This will install the app. Now you should be able to interact with the project by typing **guild** in your terminal.

## Terminal UI

The TUI has the following keybindings:

| Key | Action |
|---|---|
| `Enter` | Send message |
| `Ctrl+Y` | Copy latest code block |
| `Ctrl+R` | Toggle reasoning/progress panel |
| `Ctrl+L` | Clear chat and reset conversation history |
| `Escape` | Return focus to input |
| `Ctrl+C` | Quit |

Additional UI behavior:

- Assistant messages render code blocks in a dedicated visual block.
- `Ctrl+Y` copies the most recent code block returned by the assistant.
- If clipboard access is unavailable, copied code is saved to a temporary file in your OS temp directory (file name: `guild_copy.txt`).
- The reasoning panel shows live progress events (thinking, reading, writing, updating, done, error) while a request is running.

## File Editing

guild can read and modify files in your project. Simply ask it naturally:

- _"Add error handling to llm/ollama.go"_
- _"Refactor the Ask function in llm/gemini.go"_
- _"Create a new file called tools/parser.go with..."_

guild will automatically read the relevant file, apply the change, and confirm what it did. The conversation history is maintained across messages so follow up instructions like _"do it again"_ or _"also add a comment"_ work as expected.

Use `Ctrl+L` to clear the conversation history when starting a new unrelated task, this also reduces token usage and keeps costs low.

## How edits are applied

guild supports three editing actions under the hood:

- `read_file`: reads a target file before making changes.
- `write_file`: rewrites a full file with updated content.
- `replace_in_file`: applies a targeted exact text replacement.

Important behavior:

- For reliability, edits are performed in steps (read first, then write/replace).
- `replace_in_file` only succeeds when the exact `old` text is found.
- Successful writes/updates refresh project context automatically.
- Written files normalize line endings to LF (`\n`) for consistency.

## Context and token management

- At startup, guild scans the project and builds a file context automatically.
- Common heavy/noisy directories and binary-like extensions are ignored during context building.
- Large file reads are truncated (currently at 8000 bytes) with a truncation notice.
- After a file is updated, stale in-memory file content is evicted from the active conversation to reduce token usage.

## Model Strategy

guild is model-agnostic. The backend supports multiple AI providers through a flexible architecture, switching models requires only a change in environment variables. 

Provider defaults and fallback behavior:

- If `LLM_PROVIDER` is missing or unknown, guild falls back to Ollama.
- Default model per provider when `LLM_MODEL` is not set:
  - Ollama: `deepseek-coder`
  - Gemini: `gemini-2.0-flash`
  - Claude: `claude-sonnet-4-6`
  - OpenAI: `gpt-4o`
- API key checks are enforced for cloud providers (`ANTHROPIC_API_KEY`, `GEMINI_API_KEY`, `OPENAI_API_KEY`).
