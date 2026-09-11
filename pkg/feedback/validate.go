// validate.go 复刻官方 message-feedback 的值约束：评分只接受 thumbs up/down 两值
// （0 表示未评分，不作为有效反馈落库）；note 若提供则必须含非空白字符。
package feedback

import (
	"errors"
	"strings"
)

// ErrInvalidRating 评分不是有效两值之一。
var ErrInvalidRating = errors.New("feedback: rating must be thumbs up or down")

// ErrEmptyNote note 提供了但全为空白。
var ErrEmptyNote = errors.New("feedback: note must contain a non-whitespace character")

// ValidateRating 校验评分只取 ThumbsUp/ThumbsDown。
func ValidateRating(r Rating) error {
	if r != RatingThumbsUp && r != RatingThumbsDown {
		return ErrInvalidRating
	}
	return nil
}

// ValidateNote note 非空时要求含非空白字符；空串视为"不提供 note"，合法。
func ValidateNote(note string) error {
	if note == "" {
		return nil
	}
	if strings.TrimSpace(note) == "" {
		return ErrEmptyNote
	}
	return nil
}
