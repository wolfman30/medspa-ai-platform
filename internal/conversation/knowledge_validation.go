package conversation

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

type knowledgeDocumentPayload struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

// ParseKnowledgePayload accepts either []string or []{title, content}.
func ParseKnowledgePayload(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 {
		return nil, errors.New("documents required")
	}

	var documents []string
	if err := json.Unmarshal(raw, &documents); err == nil {
		return trimKnowledgeDocuments(documents), nil
	}

	var titled []knowledgeDocumentPayload
	if err := json.Unmarshal(raw, &titled); err != nil {
		return nil, errors.New("documents must be an array of strings or {title, content} objects")
	}

	documents = make([]string, 0, len(titled))
	for _, doc := range titled {
		title := strings.TrimSpace(doc.Title)
		content := strings.TrimSpace(doc.Content)
		switch {
		case title != "" && content != "":
			documents = append(documents, title+"\n\n"+content)
		case content != "":
			documents = append(documents, content)
		case title != "":
			documents = append(documents, title)
		}
	}
	return trimKnowledgeDocuments(documents), nil
}

// ValidateKnowledgeDocuments enforces length limits and blocks PHI.
func ValidateKnowledgeDocuments(documents []string) error {
	if len(documents) == 0 {
		return errors.New("documents required")
	}
	const maxDocs = 20
	if len(documents) > maxDocs {
		return fmt.Errorf("maximum %d documents per request", maxDocs)
	}

	for _, doc := range documents {
		if strings.TrimSpace(doc) == "" {
			return errors.New("documents must be non-empty strings")
		}
		if containsPHI(doc) {
			return errors.New("documents cannot contain PHI")
		}
	}
	return nil
}

func trimKnowledgeDocuments(documents []string) []string {
	trimmed := make([]string, 0, len(documents))
	for _, doc := range documents {
		value := strings.TrimSpace(doc)
		if value != "" {
			trimmed = append(trimmed, value)
		}
	}
	return trimmed
}

var (
	phiSSNRegex = regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`)
	phiDOBRegex = regexp.MustCompile(`(?i)\b(?:dob|date of birth)\b.*\b\d{1,2}[/-]\d{1,2}[/-]\d{2,4}\b`)
)

func containsPHI(message string) bool {
	lower := strings.ToLower(message)
	if detectPHI(lower) {
		return true
	}
	if phiSSNRegex.MatchString(message) {
		return true
	}
	if phiDOBRegex.MatchString(lower) {
		return true
	}
	phiHints := []string{
		"patient name",
		"patient:",
		"client name",
		"client:",
		"ssn",
		"social security",
		"medical record",
		"mrn",
		"insurance policy",
		"member id",
	}
	for _, hint := range phiHints {
		if strings.Contains(lower, hint) {
			return true
		}
	}
	return false
}
