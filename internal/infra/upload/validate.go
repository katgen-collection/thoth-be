// Package upload validates user-uploaded files before they are stored. It trusts
// nothing from the client: type is determined by magic bytes, the filename is
// sanitized, and size is bounded.
package upload

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"strings"
	"unicode"
)

var (
	ErrEmpty        = errors.New("file is empty")
	ErrTooLarge     = errors.New("file exceeds maximum allowed size")
	ErrNotPDF       = errors.New("file is not a valid PDF")
	ErrEncryptedPDF = errors.New("encrypted/password-protected PDFs are not supported")
)

// pdfMagic is the required leading signature of every PDF.
var pdfMagic = []byte("%PDF-")

// Validated is the result of a successful validation.
type Validated struct {
	MimeType string // always "application/pdf" here
	SHA256   string // hex-encoded content hash
	Filename string // sanitized, display-only original name
	Size     int64
}

// ValidatePDF checks an in-memory upload. data must be the full file bytes
// (callers enforce the size cap while reading; maxBytes is the hard backstop).
func ValidatePDF(data []byte, originalName string, maxBytes int64) (Validated, error) {
	if len(data) == 0 {
		return Validated{}, ErrEmpty
	}
	if int64(len(data)) > maxBytes {
		return Validated{}, ErrTooLarge
	}
	if !bytes.HasPrefix(data, pdfMagic) {
		return Validated{}, ErrNotPDF
	}
	if looksEncrypted(data) {
		return Validated{}, ErrEncryptedPDF
	}

	sum := sha256.Sum256(data)
	return Validated{
		MimeType: "application/pdf",
		SHA256:   hex.EncodeToString(sum[:]),
		Filename: SanitizeFilename(originalName),
		Size:     int64(len(data)),
	}, nil
}

// looksEncrypted is a cheap heuristic: a standard encrypted PDF references an
// /Encrypt entry in its trailer. This catches the common case; pdfplumber will
// also reject anything that slips through, and that path marks the CV failed.
func looksEncrypted(data []byte) bool {
	tail := data
	const window = 4096
	if len(tail) > window {
		tail = tail[len(tail)-window:]
	}
	return bytes.Contains(tail, []byte("/Encrypt"))
}

// SanitizeFilename produces a safe, display-only filename. It strips any path
// components, control characters, and limits length. It never returns a name
// usable for path traversal; an empty/odd input falls back to "upload.pdf".
func SanitizeFilename(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))

	var b strings.Builder
	for _, r := range name {
		switch {
		case r == 0, r == '\n', r == '\r', r == '/', unicode.IsControl(r):
			// drop
		default:
			b.WriteRune(r)
		}
	}
	clean := strings.TrimSpace(b.String())
	clean = strings.TrimLeft(clean, ".") // no leading dots (hidden/relative)

	const maxLen = 200
	if len(clean) > maxLen {
		clean = clean[:maxLen]
	}
	if clean == "" {
		return "upload.pdf"
	}
	return clean
}
