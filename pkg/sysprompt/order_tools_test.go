package sysprompt

import (
	"reflect"
	"testing"
)

func TestOrderToolsDefault(t *testing.T) {
	got, err := OrderTools([]string{"c", "a", "b"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Fatalf("got = %v", got)
	}
}

func TestOrderToolsWithRest(t *testing.T) {
	got, err := OrderTools([]string{"read", "bash", "edit"}, []string{"bash", ToolOrderRest})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"bash", "edit", "read"}) {
		t.Fatalf("got = %v", got)
	}
}

func TestOrderToolsMissingRest(t *testing.T) {
	if _, err := OrderTools([]string{"a"}, []string{"a"}); err == nil {
		t.Fatal("missing rest must error")
	}
}

func TestOrderToolsUnknown(t *testing.T) {
	if _, err := OrderTools([]string{"a"}, []string{"nope", ToolOrderRest}); err == nil {
		t.Fatal("unknown tool must error")
	}
}

func TestOrderToolsDuplicate(t *testing.T) {
	if _, err := OrderTools([]string{"a"}, []string{"a", "a", ToolOrderRest}); err == nil {
		t.Fatal("duplicate must error")
	}
}
