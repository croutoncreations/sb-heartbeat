package security_test

import (
	"testing"

	"github.com/croutoncreations/sb-heartbeat/internal/security"
)

func TestValidateHostedProjectURL(t *testing.T) {
	got, err := security.ValidateHostedProjectURL("https://abcdefghijklmnopqrst.supabase.co/")
	if err != nil {
		t.Fatalf("ValidateHostedProjectURL() error = %v", err)
	}
	if got.String() != "https://abcdefghijklmnopqrst.supabase.co" {
		t.Fatalf("normalized URL = %q", got)
	}
}

func TestValidateHostedProjectURLRejectsUnsafeShapes(t *testing.T) {
	inputs := []string{
		"http://abcdefghijklmnopqrst.supabase.co",
		"https://user@abcdefghijklmnopqrst.supabase.co",
		"https://abcdefghijklmnopqrst.supabase.co:443",
		"https://abcdefghijklmnopqrst.supabase.co/rest/v1",
		"https://abcdefghijklmnopqrst.supabase.co?x=1",
		"https://abcdefghijklmnopqrst.supabase.co?",
		"https://abcdefghijklmnopqrst.supabase.co#x",
		"https://abcdefghijklmnopqrst.supabase.co#",
		"https://abcdefghijklmnopqrst.supabase.co:",
		"https://abcdefghijklmnopqrst.supabase.co.example.com",
		"https://extra.abcdefghijklmnopqrst.supabase.co",
		"https://ABC.supabase.co",
		"https://127.0.0.1.supabase.co",
		"https://abcdefghijklmnopqrst.supabase.co.",
	}
	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			if _, err := security.ValidateHostedProjectURL(input); err == nil {
				t.Fatal("error = nil, want rejection")
			}
		})
	}
}
