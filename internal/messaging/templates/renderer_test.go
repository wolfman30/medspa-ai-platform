package templates

import "testing"

func TestRendererRender(t *testing.T) {
	r := Renderer{}
	out, err := r.Render("greet", "Hello {{.Name}}", map[string]string{"Name": "Patient"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if out != "Hello Patient" {
		t.Fatalf("unexpected output %q", out)
	}
	if _, err := r.Render("bad", "Hello {{.Missing}}", map[string]string{"Name": "x"}); err == nil {
		t.Fatalf("expected error for missing key")
	}
}
