// Package tests 的 uuid v4 验收测试。
package tests

import (
	"regexp"
	"testing"

	"github.com/JopenChen/dsh-go/pkg/uuid"
)

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestNewV4Format(t *testing.T) {
	id, err := uuid.NewV4()
	if err != nil {
		t.Fatal(err)
	}
	if !uuidRe.MatchString(id) {
		t.Fatalf("uuid not canonical v4: %q", id)
	}
}

func TestNewV4Unique(t *testing.T) {
	a, _ := uuid.NewV4()
	b, _ := uuid.NewV4()
	if a == b {
		t.Fatal("two uuids must differ")
	}
}
