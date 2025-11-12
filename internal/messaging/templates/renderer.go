package templates

import (
	"bytes"
	"fmt"
	"text/template"
)

// Renderer renders small text templates for outbound messaging.
type Renderer struct{}

// Render compiles the provided template text with strict missing-key semantics.
func (Renderer) Render(name, tmpl string, data any) (string, error) {
	if tmpl == "" {
		return "", fmt.Errorf("templates: template text required")
	}
	t, err := template.New(name).Option("missingkey=error").Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("templates: parse: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("templates: execute: %w", err)
	}
	return buf.String(), nil
}
