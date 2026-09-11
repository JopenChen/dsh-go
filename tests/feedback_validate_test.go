// Package tests 的 feedback 值约束验收测试。
package tests

import (
	"testing"

	"github.com/JopenChen/dsh-go/pkg/feedback"
)

func TestValidateRating(t *testing.T) {
	if err := feedback.ValidateRating(feedback.RatingThumbsUp); err != nil {
		t.Fatal(err)
	}
	if err := feedback.ValidateRating(feedback.RatingThumbsDown); err != nil {
		t.Fatal(err)
	}
	if err := feedback.ValidateRating(feedback.RatingNone); err != feedback.ErrInvalidRating {
		t.Fatalf("none rating rejected, got %v", err)
	}
}

func TestValidateNote(t *testing.T) {
	if err := feedback.ValidateNote(""); err != nil {
		t.Fatal("empty note means absent and is legal")
	}
	if err := feedback.ValidateNote("good"); err != nil {
		t.Fatal(err)
	}
	if err := feedback.ValidateNote("   "); err != feedback.ErrEmptyNote {
		t.Fatalf("whitespace note rejected, got %v", err)
	}
}
