package ingest

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// ScanFiles recursively walks a directory; returns text files and skip-list.
func ScanFiles(dir string) ([]RawFile, []string) {
	var files []RawFile
	var skipped []string

	extBlock := map[string]bool{
		".exe": true, ".dll": true, ".so": true, ".bin": true, ".dat": true,
		".db": true, ".sqlite": true, ".zip": true, ".tar": true, ".gz": true,
		".7z": true, ".rar": true, ".png": true, ".jpg": true, ".jpeg": true,
		".gif": true, ".ico": true, ".bmp": true, ".mp3": true, ".mp4": true,
		".avi": true, ".mov": true, ".wav": true, ".ttf": true, ".otf": true,
		".lock": true, ".sum": true, ".woff": true, ".woff2": true,
	}
	skipDirs := map[string]bool{
		"node_modules": true, ".git": true, "__pycache__": true,
		"vendor": true, "venv": true, ".venv": true,
		"dist": true, "build": true, "target": true,
	}

	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			skipped = append(skipped, path)
			return nil
		}
		if strings.HasPrefix(info.Name(), ".") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			if skipDirs[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(info.Name()))
		if extBlock[ext] {
			return nil
		}
		if info.Size() > 20*1024*1024 {
			skipped = append(skipped, info.Name()+" (过大)")
			return nil
		}
		if info.Size() == 0 {
			return nil
		}
		files = append(files, RawFile{
			Path: path, Name: info.Name(), Ext: ext, Size: info.Size(),
		})
		return nil
	})
	return files, skipped
}

// FileHashAndPreview returns hex hash (first 16 chars of SHA-256) and
// the first 200 chars of text content.
func FileHashAndPreview(path string) (hash string, preview string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", ""
	}
	h := sha256.Sum256(data)
	hash = hex.EncodeToString(h[:])[:16]
	text := string(data)
	if IsBinary(data) {
		preview = "(binary/PDF)"
	} else {
		preview = text
		if len(preview) > 200 {
			preview = preview[:200]
		}
	}
	return
}

// IsBinary returns true if the first 1KB is not valid UTF-8.
func IsBinary(data []byte) bool {
	if len(data) > 1024 {
		data = data[:1024]
	}
	return !utf8.Valid(data)
}
