package tools

import "guild/prompt"

var ignoredDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true,
	"dist": true, "build": true, ".idea": true,
}

var ignoredExts = map[string]bool{
	".exe": true, ".bin": true, ".png": true, ".jpg": true,
	".zip": true, ".sum": true,
}

func BuildFileTree(root string) ([]string, error) {
	entries, err := prompt.BuildFileList(root)
	if err != nil {
		return nil, err
	}
	paths := make([]string, len(entries))
	for i, e := range entries {
		paths[i] = e.RelPath
	}
	return paths, nil
}
