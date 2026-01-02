package conversation

// RedactPHI returns a redacted placeholder when PHI is detected.
func RedactPHI(message string) (string, bool) {
	if detectPHI(message) {
		return "[REDACTED]", true
	}
	return message, false
}

// RedactSensitive returns a redacted placeholder when PHI or medical advice requests are detected.
func RedactSensitive(message string) (string, bool) {
	if detectPHI(message) {
		return "[REDACTED]", true
	}
	if len(detectMedicalAdvice(message)) > 0 {
		return "[REDACTED]", true
	}
	return message, false
}
