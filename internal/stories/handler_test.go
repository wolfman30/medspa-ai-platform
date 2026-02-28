package stories

import (
	"testing"
)

func TestCreateStoryRequestDefaults(t *testing.T) {
	req := CreateStoryRequest{Title: "Test"}
	if req.Status != "" {
		t.Errorf("expected empty default status, got %q", req.Status)
	}
	if req.Priority != "" {
		t.Errorf("expected empty default priority, got %q", req.Priority)
	}
}

func TestUpdateStoryRequestPartial(t *testing.T) {
	title := "New Title"
	req := UpdateStoryRequest{Title: &title}
	if req.Title == nil || *req.Title != "New Title" {
		t.Errorf("expected title to be set")
	}
	if req.Status != nil {
		t.Errorf("expected status to be nil for partial update")
	}
}
