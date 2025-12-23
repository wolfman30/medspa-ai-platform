package compliance

import (
	"regexp"
	"strings"
)

// Detector identifies STOP/HELP keywords in inbound messages.
type Detector struct {
	stopRegex *regexp.Regexp
	helpRegex *regexp.Regexp
	startRegex *regexp.Regexp
}

// NewDetector returns a keyword detector with sane defaults.
func NewDetector() *Detector {
	return &Detector{
		stopRegex:  regexp.MustCompile(`(?i)^(?:please\s+)?(stop|stopall|unsubscribe|cancel|end|quit)\b`),
		helpRegex:  regexp.MustCompile(`(?i)^(?:please\s+)?(help|info)\b`),
		startRegex: regexp.MustCompile(`(?i)^(?:please\s+)?(start|unstop|subscribe)\b`),
	}
}

// IsStop returns true when body contains a STOP keyword.
func (d *Detector) IsStop(body string) bool {
	if d == nil || d.stopRegex == nil {
		return false
	}
	return d.stopRegex.MatchString(strings.TrimSpace(body))
}

// IsHelp returns true when body contains a HELP keyword.
func (d *Detector) IsHelp(body string) bool {
	if d == nil || d.helpRegex == nil {
		return false
	}
	return d.helpRegex.MatchString(strings.TrimSpace(body))
}

// IsStart returns true when body contains a START keyword (opt-in).
func (d *Detector) IsStart(body string) bool {
	if d == nil || d.startRegex == nil {
		return false
	}
	return d.startRegex.MatchString(strings.TrimSpace(body))
}
