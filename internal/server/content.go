package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

type HTMLStore interface {
	LoadHTML(context.Context) (string, error)
	SaveHTML(context.Context, string) error
}

type FileHTMLStore struct {
	path string
}

func NewFileHTMLStore(path string) *FileHTMLStore {
	return &FileHTMLStore{path: path}
}

func (s *FileHTMLStore) LoadHTML(context.Context) (string, error) {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read public catalog html: %w", err)
	}
	return string(data), nil
}

func (s *FileHTMLStore) SaveHTML(_ context.Context, html string) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create public catalog html directory: %w", err)
	}
	if err := os.WriteFile(s.path, []byte(html), 0o600); err != nil {
		return fmt.Errorf("write public catalog html: %w", err)
	}
	return nil
}
