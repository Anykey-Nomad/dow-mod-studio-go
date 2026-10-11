package main

import (
	"strings"
	"testing"
)

func TestHighlightSyntaxTokensLua(t *testing.T) {
	tokens, lines := highlightSyntaxTokens("local answer = 42 -- done\nprint(\"ok\")", ".lua")
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}

	want := map[string]syntaxKind{
		"local":                  syntaxKeyword,
		"answer":                 syntaxPlain,
		"42":                     syntaxNumber,
		"-- done\nprint(\"ok\")": syntaxComment,
	}
	for _, token := range tokens {
		if kind, ok := want[token.text]; ok && kind != token.kind {
			t.Errorf("token %q kind = %d, want %d", token.text, token.kind, kind)
		}
	}
}

func TestHighlightSyntaxTokensDoesNotMergeCommentLines(t *testing.T) {
	tokens, _ := highlightSyntaxTokens("-- first comment\nlocal value = 1\n-- second comment\nreturn value\n", ".lua")
	for _, token := range tokens {
		if token.kind == syntaxComment && strings.Contains(token.text, "\n") {
			t.Fatalf("comment token crosses line boundary: %q", token.text)
		}
	}
	if len(tokens) == 0 {
		t.Fatal("expected tokens for multi-line source")
	}
}

func TestHighlightSyntaxTokensRat(t *testing.T) {
	tokens, _ := highlightSyntaxTokens("set value = 0x10 // comment", ".rat")
	found := map[string]syntaxKind{}
	for _, token := range tokens {
		found[token.text] = token.kind
	}
	if found["set"] != syntaxKeyword {
		t.Errorf("set kind = %d, want keyword", found["set"])
	}
	if found["0x10"] != syntaxNumber {
		t.Errorf("number kind = %d, want number", found["0x10"])
	}
	if found["// comment"] != syntaxComment {
		t.Errorf("comment kind = %d, want comment", found["// comment"])
	}
}
