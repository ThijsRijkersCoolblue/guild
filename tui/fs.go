package tui

import (
	"os"
	"strings"
)

func ReadFileOS(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func WriteFileOS(path string, data []byte) error {
	normalized := strings.ReplaceAll(string(data), "\r\n", "\n")
	return os.WriteFile(path, []byte(normalized), 0644)
}
