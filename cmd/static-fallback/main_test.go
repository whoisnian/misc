package main

import (
	"testing"

	iradix "github.com/hashicorp/go-immutable-radix/v2"
)

func buildTree(fallbackFiles []string) *iradix.Tree[string] {
	tree := iradix.New[string]()
	for _, f := range fallbackFiles {
		buf := reverseBytesFromString(f)
		tree, _, _ = tree.Insert(buf, f)
	}
	return tree
}

func lookupFallback(tree *iradix.Tree[string], input string) (string, bool) {
	buf := reverseBytesFromString(input)
	_, val, ok := tree.Root().LongestPrefix(buf)
	return val, ok
}

func TestFallbackMatchSingleLevel(t *testing.T) {
	tree := buildTree([]string{
		"index.html",
	})

	tests := []struct {
		input string
		want  string
	}{
		{"base/index.html", "index.html"},
		{"base/other/index.html", "index.html"},
		{"base/a/b/c/index.html", "index.html"},
	}
	for _, tt := range tests {
		got, ok := lookupFallback(tree, tt.input)
		if !ok {
			t.Errorf("lookupFallback(%q): not match, want %q", tt.input, tt.want)
		} else if got != tt.want {
			t.Errorf("lookupFallback(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFallbackMatchMultiLevel(t *testing.T) {
	tree := buildTree([]string{
		"index.html",
		"app/index.html",
		"app/admin/index.html",
	})

	tests := []struct {
		input string
		want  string
	}{
		{input: "base/index.html", want: "index.html"},
		{input: "base/app/index.html", want: "app/index.html"},
		{input: "base/app/admin/index.html", want: "app/admin/index.html"},
		{input: "base/xyz/app/index.html", want: "app/index.html"},
		{input: "base/xyz/app/admin/index.html", want: "app/admin/index.html"},
		{input: "base/other/index.html", want: "index.html"},
	}
	for _, tt := range tests {
		got, ok := lookupFallback(tree, tt.input)
		if !ok {
			t.Errorf("lookupFallback(%q): not match, want %q", tt.input, tt.want)
		} else if got != tt.want {
			t.Errorf("lookupFallback(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFallbackMatchDifferentFiles(t *testing.T) {
	tree := buildTree([]string{
		"index.html",
		"app/index.html",
		"style.css",
		"app/style.css",
	})

	tests := []struct {
		input string
		want  string
	}{
		{input: "base/app/index.html", want: "app/index.html"},
		{input: "base/app/style.css", want: "app/style.css"},
		{input: "base/other/style.css", want: "style.css"},
		{input: "base/other/index.html", want: "index.html"},
	}
	for _, tt := range tests {
		got, ok := lookupFallback(tree, tt.input)
		if !ok {
			t.Errorf("lookupFallback(%q): not match, want %q", tt.input, tt.want)
		} else if got != tt.want {
			t.Errorf("lookupFallback(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFallbackNoMatch(t *testing.T) {
	tree := buildTree([]string{
		"index.html",
		"app/index.html",
	})

	_, ok := lookupFallback(tree, "base/app/logo.png")
	if ok {
		t.Error("lookupFallback(base/app/logo.png): unexpected match for unregistered file")
	}
}
