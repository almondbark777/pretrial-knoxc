package main

import (
	"html/template"
	"path/filepath"
	"testing"
)

// TestTemplatesParse parses every real template with the production funcmap, the
// same way main() does at startup. Unit tests elsewhere use stub templates, so
// this is the guard that a syntax error in a *.html (e.g. a bad calendar/modal
// edit) is caught by `go test` instead of at server boot.
func TestTemplatesParse(t *testing.T) {
	if _, err := template.New("").Funcs(tmplFuncs()).ParseGlob(filepath.Join("..", "..", "templates", "*.html")); err != nil {
		t.Fatalf("ParseGlob templates: %v", err)
	}
}
