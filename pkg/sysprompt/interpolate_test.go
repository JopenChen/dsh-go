package sysprompt

import "testing"

func TestInterpolateOK(t *testing.T) {
	got, err := Interpolate("hi {{name}}, age {{age}}", map[string]string{"name": "bob", "age": "3"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "hi bob, age 3" {
		t.Fatalf("got = %q", got)
	}
}

func TestInterpolateUnknown(t *testing.T) {
	if _, err := Interpolate("{{x}}", map[string]string{}); err == nil {
		t.Fatal("unknown variable must error")
	}
}

func TestInterpolateBadName(t *testing.T) {
	if _, err := Interpolate("{{X}}", map[string]string{"X": "1"}); err == nil {
		t.Fatal("bad name must error")
	}
}

func TestInterpolateNoRescan(t *testing.T) {
	got, err := Interpolate("{{a}}", map[string]string{"a": "{{other}}"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "{{other}}" {
		t.Fatalf("got = %q", got)
	}
}

func TestInterpolateLoneOpen(t *testing.T) {
	got, err := Interpolate("a {{ b", map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if got != "a {{ b" {
		t.Fatalf("got = %q", got)
	}
}
