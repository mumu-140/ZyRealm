package modelmatch

import (
	"reflect"
	"testing"
)

func TestFilterEmptyPatternsPreserveModelsAndOrder(t *testing.T) {
	models := []string{"gpt-z", "claude-a", "gpt-z"}

	got, err := Filter(models, "", "   ")
	if err != nil {
		t.Fatalf("Filter() error = %v", err)
	}
	if !reflect.DeepEqual(got, models) {
		t.Fatalf("Filter() = %#v, want %#v", got, models)
	}
}

func TestFilterSinglePattern(t *testing.T) {
	models := []string{"claude-3-5-sonnet", "gpt-4o", "gpt-4.1", "gemini-2.5-pro"}

	got, err := Filter(models, `^gpt-`)
	if err != nil {
		t.Fatalf("Filter() error = %v", err)
	}
	want := []string{"gpt-4o", "gpt-4.1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Filter() = %#v, want %#v", got, want)
	}
}

func TestFilterPatternsComposeAsIntersection(t *testing.T) {
	models := []string{"gpt-4o", "gpt-4o-mini", "claude-mini", "gpt-4.1-mini"}

	got, err := Filter(models, `^gpt-`, `mini$`)
	if err != nil {
		t.Fatalf("Filter() error = %v", err)
	}
	want := []string{"gpt-4o-mini", "gpt-4.1-mini"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Filter() = %#v, want %#v", got, want)
	}
}

func TestFilterPreservesInputOrder(t *testing.T) {
	models := []string{"gpt-4.1-mini", "gpt-4o", "gpt-4o-mini", "gpt-4.1"}

	got, err := Filter(models, `^gpt-`, `mini$`)
	if err != nil {
		t.Fatalf("Filter() error = %v", err)
	}
	want := []string{"gpt-4.1-mini", "gpt-4o-mini"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Filter() = %#v, want %#v", got, want)
	}
}

func TestFilterInvalidPatternReturnsError(t *testing.T) {
	if _, err := Filter([]string{"gpt-4o"}, `(`); err == nil {
		t.Fatal("Filter() error = nil, want invalid-pattern error")
	}
}

func TestValidateUsesExistingECMAScriptDialect(t *testing.T) {
	if err := Validate(`^gpt-(4o|4\\.1)$`); err != nil {
		t.Fatalf("Validate() valid ECMAScript pattern error = %v", err)
	}
	// regexp2 RE2 mode accepts possessive quantifiers, while ECMAScript mode
	// deliberately keeps them invalid. Channel MatchRegex currently uses
	// regexp2.ECMAScript, so the shared matcher must preserve that dialect.
	if err := Validate(`a++`); err == nil {
		t.Fatal("Validate() accepted possessive quantifier; want ECMAScript-compatible rejection")
	}
}
