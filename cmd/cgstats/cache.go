package main

import (
	"io"
	"os"
)

type FileCache struct {
	m map[string]*os.File
}

func NewFileCache() *FileCache {
	return &FileCache{
		m: make(map[string]*os.File),
	}
}

func (fc *FileCache) CloseAll() {
	for _, fi := range fc.m {
		fi.Close()
	}
}

func (fc *FileCache) SeekAndReadAll(path string) ([]byte, error) {
	fi, err := fc.open(path)
	if err != nil {
		return nil, err
	}
	_, err = fi.Seek(0, 0)
	if err != nil {
		return nil, err
	}
	return io.ReadAll(fi)
}

func (fc *FileCache) open(path string) (*os.File, error) {
	if fi, ok := fc.m[path]; ok {
		return fi, nil
	}
	fi, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	fc.m[path] = fi
	return fi, nil
}
