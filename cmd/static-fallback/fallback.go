package main

import (
	"context"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	iradix "github.com/hashicorp/go-immutable-radix/v2"
)

type fallbackFiles struct {
	dirs []string
	tree *iradix.Tree[string]
	mu   *sync.RWMutex
}

func NewFallbackFiles(dirs []string) *fallbackFiles {
	return &fallbackFiles{
		dirs: dirs,
		tree: iradix.New[string](),
		mu:   new(sync.RWMutex),
	}
}

func (f *fallbackFiles) Size() int {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.tree.Len()
}

func (f *fallbackFiles) LongestSuffix(s string) (key string, val string, ok bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	buf := reverseBytesFromString(s)
	k, v, ok := f.tree.Root().LongestPrefix(buf)
	return string(k), v, ok
}

func (f *fallbackFiles) Rebuild(ctx context.Context) error {
	newTree := iradix.New[string]()
	for _, dir := range f.dirs {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		LOG.Debugf(ctx, "walk fallback directory: %s", dir)
		if err := walkDirAndBuildTree(ctx, &newTree, dir); err != nil {
			return err
		}
	}

	f.mu.Lock()
	f.tree = newTree
	f.mu.Unlock()
	return nil
}

func walkDirAndBuildTree(ctx context.Context, tree **iradix.Tree[string], root string) error {
	walkDirFunc := func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		buf := reverseBytesFromString(relPath)
		LOG.Debugf(ctx, "discover %s as %s", path, buf)
		*tree, _, _ = (*tree).Insert(buf, path)
		return nil
	}
	return filepath.WalkDir(root, walkDirFunc)
}

func reverseBytesFromString(s string) []byte {
	buf := []byte(s)
	slices.Reverse(buf)
	return buf
}
