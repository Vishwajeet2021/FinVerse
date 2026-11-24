package domain

import (
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// ErrInvalidInput marks input that fails a business validation rule. The wrapped message is
// written for the customer and never contains internals.
var ErrInvalidInput = errors.New("invalid input")

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidInput, fmt.Sprintf(format, args...))
}

// panPattern is the Income Tax Department format: five letters, four digits, one letter.
var panPattern = regexp.MustCompile(`^[A-Z]{5}[0-9]{4}[A-Z]$`)

const (
	MaxFullNameRunes = 120
	MinAge           = 18
	MaxAge           = 120

	// MaxDocumentBytes caps a single KYC upload. Scans of identity documents fit well under it.
	MaxDocumentBytes = 5 << 20
)

// ValidatePAN requires the normalised (upper-case) PAN format.
func ValidatePAN(pan string) error {
	if !panPattern.MatchString(pan) {
		return invalid("PAN must be 10 characters in the format ABCDE1234F")
	}
	return nil
}

// ValidateFullName accepts letters, marks, spaces and the punctuation real names use, and
// rejects control characters and absurd lengths.
func ValidateFullName(name string) error {
	name = strings.TrimSpace(name)
	n := utf8.RuneCountInString(name)
	if n < 2 || n > MaxFullNameRunes {
		return invalid("full name must be between 2 and %d characters", MaxFullNameRunes)
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f || r == '<' || r == '>' {
			return invalid("full name contains characters that are not allowed")
		}
	}
	return nil
}

// ValidateDOB requires a past date that makes the customer an adult and not implausibly old.
func ValidateDOB(dob, now time.Time) error {
	if !dob.Before(now) {
		return invalid("date of birth must be in the past")
	}
	if dob.After(now.AddDate(-MinAge, 0, 0)) {
		return invalid("you must be at least %d years old", MinAge)
	}
	if dob.Before(now.AddDate(-MaxAge, 0, 0)) {
		return invalid("date of birth is not valid")
	}
	return nil
}

var allowedDocTypes = map[string]bool{"PAN": true, "AADHAAR": true, "PASSPORT": true, "DRIVING_LICENCE": true, "VOTER_ID": true}

// allowedContentTypes maps the content types a KYC scan may have to the extensions permitted
// for them. The type comes from sniffing the bytes, never from the client's claim.
var allowedContentTypes = map[string][]string{
	"application/pdf": {".pdf"},
	"image/png":       {".png"},
	"image/jpeg":      {".jpg", ".jpeg"},
}

// ValidateDocument checks the type label, size, and that the bytes really are a PDF, PNG or
// JPEG whose extension agrees. It returns a sanitised extension for storage.
func ValidateDocument(docType, filename string, data []byte) (ext string, err error) {
	if !allowedDocTypes[docType] {
		return "", invalid("unsupported document type")
	}
	if len(data) == 0 {
		return "", invalid("the document is empty")
	}
	if len(data) > MaxDocumentBytes {
		return "", invalid("the document must be smaller than %d MB", MaxDocumentBytes>>20)
	}
	if utf8.RuneCountInString(filename) > 128 {
		return "", invalid("filename is too long")
	}
	contentType := http.DetectContentType(data)
	exts, ok := allowedContentTypes[contentType]
	if !ok {
		return "", invalid("only PDF, PNG and JPEG documents are accepted")
	}
	got := strings.ToLower(filepath.Ext(filepath.Base(filename)))
	for _, e := range exts {
		if got == e {
			return e, nil
		}
	}
	return "", invalid("the file extension does not match the file contents")
}
