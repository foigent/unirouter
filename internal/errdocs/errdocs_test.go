package errdocs

import "testing"

func TestAll_ReturnsCopyWithRequiredFields(t *testing.T) {
	all := All()
	if len(all) == 0 {
		t.Fatal("expected registered error docs")
	}

	seen := map[string]bool{}
	for _, doc := range all {
		if doc.Code == "" {
			t.Fatal("doc code must not be empty")
		}
		if seen[doc.Code] {
			t.Fatalf("duplicate error doc code %q", doc.Code)
		}
		seen[doc.Code] = true
		if doc.Type == "" || doc.HTTPStatus == 0 || doc.Title == "" || doc.Summary == "" || doc.Resolution == "" || doc.DocURL == "" {
			t.Fatalf("doc %q has empty required fields: %#v", doc.Code, doc)
		}
		if got := URLForCode(doc.Code); got != doc.DocURL {
			t.Fatalf("URLForCode(%q)=%q, want %q", doc.Code, got, doc.DocURL)
		}
	}

	all[0].DocURL = "/mutated"
	if got := All()[0].DocURL; got == "/mutated" {
		t.Fatal("All must return a copy so callers cannot mutate registry docs")
	}
}

func TestURLForCode_Fallbacks(t *testing.T) {
	if got := URLForCode(""); got != "/docs#error-unknown" {
		t.Fatalf("empty code fallback=%q", got)
	}
	if got := URLForCode("custom_error.example"); got != "/docs#error-custom_error.example" {
		t.Fatalf("unknown code fallback=%q", got)
	}
}
