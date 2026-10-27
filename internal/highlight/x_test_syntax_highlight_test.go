package highlight_test

import (
	"dow-mod-studio/internal/highlight"
	"strings"
	"testing"
)

func TestHighlightSyntaxTokensLua(t *testing.T) {
	tokens, lines := highlight.HighlightSyntaxTokens("local answer = 42 -- done\nprint(\"ok\")", ".lua")
	if len(lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(lines))
	}

	want := map[string]highlight.SyntaxKind{
		"local":                  highlight.SyntaxKeyword,
		"answer":                 highlight.SyntaxPlain,
		"42":                     highlight.SyntaxNumber,
		"-- done\nprint(\"ok\")": highlight.SyntaxComment,
	}
	for _, token := range tokens {
		if kind, ok := want[token.Text]; ok && kind != token.Kind {
			t.Errorf("token %q kind = %d, want %d", token.Text, token.Kind, kind)
		}
	}
}

func TestHighlightSyntaxTokensDoesNotMergeCommentLines(t *testing.T) {
	tokens, _ := highlight.HighlightSyntaxTokens("-- first comment\nlocal value = 1\n-- second comment\nreturn value\n", ".lua")
	for _, token := range tokens {
		if token.Kind == highlight.SyntaxComment && strings.Contains(token.Text, "\n") {
			t.Fatalf("comment token crosses line boundary: %q", token.Text)
		}
	}
	if len(tokens) == 0 {
		t.Fatal("expected tokens for multi-line source")
	}
}

func TestHighlightSyntaxTokensRat(t *testing.T) {
	tokens, _ := highlight.HighlightSyntaxTokens("set value = 0x10 // comment", ".rat")
	found := map[string]highlight.SyntaxKind{}
	for _, token := range tokens {
		found[token.Text] = token.Kind
	}
	if found["set"] != highlight.SyntaxKeyword {
		t.Errorf("set kind = %d, want keyword", found["set"])
	}
	if found["0x10"] != highlight.SyntaxNumber {
		t.Errorf("number kind = %d, want number", found["0x10"])
	}
	if found["// comment"] != highlight.SyntaxComment {
		t.Errorf("comment kind = %d, want comment", found["// comment"])
	}
}
