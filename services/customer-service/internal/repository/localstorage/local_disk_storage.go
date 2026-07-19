// Package localstorage implements service.Storage against the local
// filesystem — a stand-in for S3 (docs/07-microservice-specifications.md §3
// names S3 as the eventual store; that's an AWS-phase concern per
// docs/19-implementation-roadmap.md Phase 4). Callers depend only on
// service.Storage, so swapping in a real S3-backed implementation later
// never touches internal/service or internal/transport.
package localstorage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

type LocalDiskStorage struct {
	basePath string
}

func NewLocalDiskStorage(basePath string) *LocalDiskStorage {
	return &LocalDiskStorage{basePath: basePath}
}

// Save writes data under basePath/{customerID}/{randomKey}{ext}. The stored
// filename is a randomized key, never the caller-supplied filename
// (docs/11-security-standards.md §5) — the original filename is not trusted
// input and is used only to derive the extension for content-type hints.
func (s *LocalDiskStorage) Save(ctx context.Context, customerID, filename string, data []byte) (string, error) {
	dir := filepath.Join(s.basePath, customerID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("creating document directory: %w", err)
	}

	key := uuid.NewString() + filepath.Ext(filename)
	fullPath := filepath.Join(dir, key)
	if err := os.WriteFile(fullPath, data, 0o600); err != nil {
		return "", fmt.Errorf("writing document to disk: %w", err)
	}

	return filepath.ToSlash(filepath.Join(customerID, key)), nil
}
