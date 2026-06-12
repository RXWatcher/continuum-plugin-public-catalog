package server

import (
	"strings"
	"testing"
)

func TestSanitizeHTMLStripsScriptAndHandlers(t *testing.T) {
	in := `<p onclick="steal()">hi</p><script>alert(1)</script><a href="javascript:alert(1)">x</a>`
	out := sanitizeHTML(in)
	if strings.Contains(strings.ToLower(out), "<script") {
		t.Fatalf("script tag survived sanitisation: %q", out)
	}
	if strings.Contains(strings.ToLower(out), "onclick") {
		t.Fatalf("event handler survived sanitisation: %q", out)
	}
	if strings.Contains(strings.ToLower(out), "javascript:") {
		t.Fatalf("javascript: URL survived sanitisation: %q", out)
	}
	if !strings.Contains(out, "hi") {
		t.Fatalf("benign text was dropped: %q", out)
	}
}

func TestSanitizeHTMLKeepsSafeFormatting(t *testing.T) {
	out := sanitizeHTML(`<p class="note"><strong>bold</strong> <a href="https://example.com">link</a></p>`)
	for _, want := range []string{"<strong>bold</strong>", `href="https://example.com"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected sanitised output to keep %q, got %q", want, out)
		}
	}
}

func TestValidRedirectURL(t *testing.T) {
	cases := map[string]bool{
		"":                        true,
		"catalog?token=abc":       true,
		"/catalog":                true,
		"https://example.com/x":   true,
		"http://example.com":      true,
		"javascript:alert(1)":     false,
		"data:text/html,<script>": false,
		"//evil.example.com/path": false,
		"vbscript:msgbox":         false,
	}
	for in, want := range cases {
		if got := validRedirectURL(in); got != want {
			t.Fatalf("validRedirectURL(%q) = %v, want %v", in, got, want)
		}
	}
}
