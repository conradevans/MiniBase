package ids

import "testing"

func TestAttachmentIDValidationAndUniqueness(t *testing.T) {
	const sampleSize = 4096
	seen := make(map[string]struct{}, sampleSize)
	for range sampleSize {
		value, err := AttachmentID()
		if err != nil {
			t.Fatalf("AttachmentID() error = %v", err)
		}
		if !ValidAttachmentID(value) {
			t.Fatalf("ValidAttachmentID(%q) = false", value)
		}
		if _, exists := seen[value]; exists {
			t.Fatalf("duplicate attachment ID generated: %q", value)
		}
		seen[value] = struct{}{}
	}

	for _, invalid := range []string{
		"",
		".",
		"..",
		"attachment_",
		"attachment_ABCDEF0123456789abcdef0123456789",
		"attachment_0123456789abcdef0123456789abcdeg",
		"../attachment_0123456789abcdef0123456789abcdef",
	} {
		if ValidAttachmentID(invalid) {
			t.Fatalf("ValidAttachmentID(%q) = true", invalid)
		}
	}
}
