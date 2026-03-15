package tui

import "strings"

func WriteFile(path, content string) error {
	return WriteFileOS(path, []byte(content))
}

func ReplaceInFile(path, old, new string) error {
	data, err := ReadFileOS(path)
	if err != nil {
		return err
	}
	updated := strings.ReplaceAll(string(data), old, new)
	return WriteFileOS(path, []byte(updated))
}
