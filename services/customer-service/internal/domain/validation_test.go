package domain

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestValidatePAN(t *testing.T) {
	for _, ok := range []string{"ABCDE1234F", "ZZZZZ9999Z"} {
		if err := ValidatePAN(ok); err != nil {
			t.Errorf("%s rejected: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "abcde1234f", "ABCDE12345", "ABCD1234FG", "ABCDE1234F ", "ABCDE1234", "ABCDE1234FF", "ABC\nDE1234F"} {
		if err := ValidatePAN(bad); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestValidateFullName(t *testing.T) {
	if err := ValidateFullName("Anaïs O'Neil-Rao"); err != nil {
		t.Errorf("legitimate name rejected: %v", err)
	}
	for _, bad := range []string{"", "A", strings.Repeat("a", MaxFullNameRunes+1), "Robert<script>", "Bad\x00Name", "Tab\tName"} {
		if err := ValidateFullName(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestValidateDOB(t *testing.T) {
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	if err := ValidateDOB(time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC), now); err != nil {
		t.Errorf("adult rejected: %v", err)
	}
	if err := ValidateDOB(now.AddDate(-18, 0, 0), now); err != nil {
		t.Errorf("exactly 18 must be accepted: %v", err)
	}
	for name, dob := range map[string]time.Time{
		"future": now.AddDate(0, 0, 1), "minor": now.AddDate(-17, 0, 0), "ancient": now.AddDate(-121, 0, 0), "today": now,
	} {
		if err := ValidateDOB(dob, now); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

var (
	pdfBytes = append([]byte("%PDF-1.4\n"), bytes.Repeat([]byte("x"), 64)...)
	pngBytes = append([]byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}, bytes.Repeat([]byte{0}, 64)...)
)

func TestValidateDocument(t *testing.T) {
	if ext, err := ValidateDocument("PAN", "scan.PDF", pdfBytes); err != nil || ext != ".pdf" {
		t.Errorf("valid pdf: (%q, %v)", ext, err)
	}
	if ext, err := ValidateDocument("AADHAAR", "front.png", pngBytes); err != nil || ext != ".png" {
		t.Errorf("valid png: (%q, %v)", ext, err)
	}
	cases := map[string]struct {
		docType, name string
		data          []byte
	}{
		"unknown doc type":        {"BIRTHDAY_CARD", "a.pdf", pdfBytes},
		"empty":                   {"PAN", "a.pdf", nil},
		"too large":               {"PAN", "a.pdf", append(append([]byte{}, pdfBytes...), make([]byte, MaxDocumentBytes)...)},
		"html pretending as pdf":  {"PAN", "a.pdf", []byte("<html><script>alert(1)</script></html>")},
		"executable as png":       {"PAN", "a.png", append([]byte("MZ"), make([]byte, 100)...)},
		"extension mismatch":      {"PAN", "a.png", pdfBytes},
		"no extension":            {"PAN", "scan", pdfBytes},
		"traversal in name":       {"PAN", "../../etc/passwd", pdfBytes},
		"very long name":          {"PAN", strings.Repeat("a", 200) + ".pdf", pdfBytes},
		"double extension attack": {"PAN", "scan.pdf.exe", pdfBytes},
	}
	for name, c := range cases {
		if _, err := ValidateDocument(c.docType, c.name, c.data); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("%s: err = %v, want ErrInvalidInput", name, err)
		}
	}
}
