package conversation

// RedactPHI returns a redacted placeholder when PHI is detected.
func RedactPHI(message string) (string, bool) {
	if detectPHI(message) {
		return "[REDACTED]", true
	}
	return message, false
}
