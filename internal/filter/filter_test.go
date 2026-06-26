package filter

import (
	"testing"
)

func TestDFAFilter_BasicMatch(t *testing.T) {
	f := NewDFAFilter([]string{"spam", "bad", "evil"})
	ok, matches := f.IsSensitive("this is spam content")
	if !ok {
		t.Fatal("should detect 'spam'")
	}
	if len(matches) != 1 || matches[0] != "spam" {
		t.Fatalf("expected [spam], got %v", matches)
	}
}

func TestDFAFilter_MultipleMatches(t *testing.T) {
	f := NewDFAFilter([]string{"spam", "bad", "evil"})
	ok, matches := f.IsSensitive("this is bad and evil content")
	if !ok {
		t.Fatal("should detect matches")
	}
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d: %v", len(matches), matches)
	}
}

func TestDFAFilter_NoMatch(t *testing.T) {
	f := NewDFAFilter([]string{"spam", "bad"})
	ok, matches := f.IsSensitive("this is clean content")
	if ok {
		t.Fatalf("should not detect, got %v", matches)
	}
}

func TestDFAFilter_CaseInsensitive(t *testing.T) {
	f := NewDFAFilter([]string{"SPAM"})
	ok, _ := f.IsSensitive("this is spam")
	if !ok {
		t.Fatal("should match case-insensitively")
	}
}

func TestDFAFilter_Unicode(t *testing.T) {
	f := NewDFAFilter([]string{"赌博", "色情"})
	ok, matches := f.IsSensitive("这里有赌博网站")
	if !ok {
		t.Fatal("should detect Chinese sensitive words")
	}
	if len(matches) != 1 || matches[0] != "赌博" {
		t.Fatalf("expected [赌博], got %v", matches)
	}
}

func TestDFAFilter_SubstringMatch(t *testing.T) {
	f := NewDFAFilter([]string{"bad"})
	ok, matches := f.IsSensitive("badword is here")
	if !ok {
		t.Fatal("should match at start of text")
	}
	if matches[0] != "bad" {
		t.Fatalf("expected 'bad', got '%s'", matches[0])
	}
}

func TestDFAFilter_Overlapping(t *testing.T) {
	f := NewDFAFilter([]string{"abc", "bcd"})
	ok, matches := f.IsSensitive("abcd")
	if !ok {
		t.Fatal("should detect overlapping words")
	}
	found := make(map[string]bool)
	for _, m := range matches {
		found[m] = true
	}
	if !found["abc"] {
		t.Fatal("should find 'abc'")
	}
}

func TestDFAFilter_UpdateWords(t *testing.T) {
	f := NewDFAFilter([]string{"old"})
	ok, _ := f.IsSensitive("this is old")
	if !ok {
		t.Fatal("should detect 'old'")
	}

	f.UpdateWords([]string{"new"})
	ok, _ = f.IsSensitive("this is old")
	if ok {
		t.Fatal("should NOT detect 'old' after update")
	}
	ok, _ = f.IsSensitive("this is new")
	if !ok {
		t.Fatal("should detect 'new' after update")
	}
}

func TestDFAFilter_EmptyText(t *testing.T) {
	f := NewDFAFilter([]string{"spam"})
	ok, _ := f.IsSensitive("")
	if ok {
		t.Fatal("empty text should not match")
	}
}

func TestDFAFilter_EmptyWordList(t *testing.T) {
	f := NewDFAFilter([]string{})
	ok, _ := f.IsSensitive("anything")
	if ok {
		t.Fatal("empty filter should not match")
	}
	if f.WordCount() != 0 {
		t.Fatalf("expected 0 words, got %d", f.WordCount())
	}
}

func TestDFAFilter_WordCount(t *testing.T) {
	f := NewDFAFilter([]string{"a", "b", "c", "d"})
	if f.WordCount() != 4 {
		t.Fatalf("expected 4, got %d", f.WordCount())
	}
}

func TestDFAFilter_Deduplication(t *testing.T) {
	f := NewDFAFilter([]string{"spam"})
	ok, matches := f.IsSensitive("spam spam spam")
	if !ok {
		t.Fatal("should detect")
	}
	if len(matches) != 1 {
		t.Fatalf("should deduplicate, got %d matches: %v", len(matches), matches)
	}
}
