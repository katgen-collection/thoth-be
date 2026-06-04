package upload

import (
	"bytes"
	"errors"
	"testing"
)

func minimalPDF() []byte {
	return []byte("%PDF-1.4\n1 0 obj<<>>endobj\ntrailer<<>>\n%%EOF")
}

func TestValidatePDF(t *testing.T) {
	const maxBytes = 1 << 20

	tests := []struct {
		name    string
		data    []byte
		want    error
	}{
		{"valid pdf", minimalPDF(), nil},
		{"empty", nil, ErrEmpty},
		{"not a pdf", []byte("GIF89a not a pdf at all"), ErrNotPDF},
		{"too large", append([]byte("%PDF-"), bytes.Repeat([]byte("x"), maxBytes+1)...), ErrTooLarge},
		{"encrypted", []byte("%PDF-1.6\nstuff\ntrailer<</Encrypt 5 0 R>>\n%%EOF"), ErrEncryptedPDF},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ValidatePDF(tc.data, "résumé.pdf", maxBytes)
			if !errors.Is(err, tc.want) {
				t.Fatalf("want err %v, got %v", tc.want, err)
			}
			if tc.want == nil {
				if got.MimeType != "application/pdf" {
					t.Errorf("mime = %q", got.MimeType)
				}
				if len(got.SHA256) != 64 {
					t.Errorf("sha256 length = %d, want 64", len(got.SHA256))
				}
			}
		})
	}
}

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"cv.pdf", "cv.pdf"},
		{"../../etc/passwd", "passwd"},
		{`..\..\windows\system32\bad.pdf`, "bad.pdf"},
		{"with\nnewline.pdf", "withnewline.pdf"},
		{"/absolute/path/résumé.pdf", "résumé.pdf"},
		{"", "upload.pdf"},
		{"...", "upload.pdf"},
		{"   ", "upload.pdf"},
	}
	for _, tc := range tests {
		if got := SanitizeFilename(tc.in); got != tc.want {
			t.Errorf("SanitizeFilename(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
