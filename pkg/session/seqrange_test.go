package session

import (
	"reflect"
	"testing"
)

func TestEncodeSeqRanges(t *testing.T) {
	// 1,2,3 连续三个 -> 区间；5 单点；7,8 不足三个 -> 单点。
	got := EncodeSeqRanges([]int{1, 2, 3, 5, 7, 8})
	want := []SeqRange{{1, 3}, {5, 5}, {7, 7}, {8, 8}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got = %v want = %v", got, want)
	}
}

func TestRoundTrip(t *testing.T) {
	src := []int{2, 3, 4, 5, 9}
	dec, err := DecodeSeqRanges(EncodeSeqRanges(src), 0)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(dec, src) {
		t.Fatalf("round trip = %v", dec)
	}
}

func TestDecodeBadRange(t *testing.T) {
	if _, err := DecodeSeqRanges([]SeqRange{{5, 3}}, 0); err == nil {
		t.Fatal("inverted range must error")
	}
}

func TestDecodeMaxEntries(t *testing.T) {
	if _, err := DecodeSeqRanges([]SeqRange{{1, 5}}, 3); err == nil {
		t.Fatal("exceeding max entries must error")
	}
}
