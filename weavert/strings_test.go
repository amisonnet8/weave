package weavert

import "testing"

func TestContains(t *testing.T) {
	if Contains("hello world", "wor") != true {
		t.Error(`Contains("hello world", "wor") should be true`)
	}
	if Contains("hello world", "xyz") != false {
		t.Error(`Contains("hello world", "xyz") should be false`)
	}
}

func TestContains_NonStringPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected Contains(1.0, \"a\") to panic")
		}
	}()
	Contains(1.0, "a")
}

func TestIndexOf_Found(t *testing.T) {
	if got := IndexOf("hello world", "world"); got != 6.0 {
		t.Errorf(`IndexOf("hello world", "world") = %v, want 6`, got)
	}
}

func TestIndexOf_NotFoundReturnsMinusOne(t *testing.T) {
	if got := IndexOf("hello world", "xyz"); got != -1.0 {
		t.Errorf(`IndexOf("hello world", "xyz") = %v, want -1`, got)
	}
}

func TestIndexOf_ReturnsARunePositionNotAByteOffset(t *testing.T) {
	// "日本語" is 3 runes but 9 UTF-8 bytes — a byte offset here would be
	// wrong for a language whose len() is defined in runes.
	if got := IndexOf("日本語world", "world"); got != 3.0 {
		t.Errorf(`IndexOf("日本語world", "world") = %v, want 3`, got)
	}
}

func TestSubstring(t *testing.T) {
	if got := Substring("hello world", 6.0, 11.0); got != "world" {
		t.Errorf(`Substring("hello world", 6, 11) = %q, want "world"`, got)
	}
}

func TestSubstring_EmptyRangeIsEmptyString(t *testing.T) {
	if got := Substring("hello", 2.0, 2.0); got != "" {
		t.Errorf(`Substring("hello", 2, 2) = %q, want ""`, got)
	}
}

func TestSubstring_CountsRunesNotBytes(t *testing.T) {
	if got := Substring("日本語world", 0.0, 3.0); got != "日本語" {
		t.Errorf(`Substring("日本語world", 0, 3) = %q, want "日本語"`, got)
	}
}

func TestSubstring_OutOfRangePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected Substring(\"hi\", 0, 5) to panic")
		}
	}()
	Substring("hi", 0.0, 5.0)
}

func TestSubstring_StartAfterEndPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected Substring(\"hi\", 2, 0) to panic")
		}
	}()
	Substring("hi", 2.0, 0.0)
}

func TestSubstring_NonWholeNumberIndexPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected Substring(\"hi\", 0.5, 1) to panic")
		}
	}()
	Substring("hi", 0.5, 1.0)
}

func TestUpperLower(t *testing.T) {
	if got := Upper("Hello"); got != "HELLO" {
		t.Errorf(`Upper("Hello") = %q, want "HELLO"`, got)
	}
	if got := Lower("Hello"); got != "hello" {
		t.Errorf(`Lower("Hello") = %q, want "hello"`, got)
	}
}

func TestTrim(t *testing.T) {
	if got := Trim("  hi there  \n"); got != "hi there" {
		t.Errorf(`Trim("  hi there  \n") = %q, want "hi there"`, got)
	}
}

func TestReplace_ReplacesAllOccurrences(t *testing.T) {
	if got := Replace("a-b-c", "-", "+"); got != "a+b+c" {
		t.Errorf(`Replace("a-b-c", "-", "+") = %q, want "a+b+c"`, got)
	}
}

func TestSplit(t *testing.T) {
	got := objOf(Split("a,b,c", ","))
	if len(got) != 3 || got[listKey(0)] != "a" || got[listKey(1)] != "b" || got[listKey(2)] != "c" {
		t.Errorf(`Split("a,b,c", ",") = %v, want ["a","b","c"]`, got)
	}
}

func TestJoin(t *testing.T) {
	list := Split("a,b,c", ",")
	if got := Join(list, "-"); got != "a-b-c" {
		t.Errorf(`Join(split result, "-") = %q, want "a-b-c"`, got)
	}
}

func TestJoin_NonStringElementPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected Join with a non-string element to panic")
		}
	}()
	list := Object{listKey(0): "a", listKey(1): 2.0}
	Join(list, ",")
}

func TestSplitThenJoin_RoundTrips(t *testing.T) {
	if got := Join(Split("one two three", " "), " "); got != "one two three" {
		t.Errorf(`round-trip through Split/Join = %q, want "one two three"`, got)
	}
}

func TestRepeat(t *testing.T) {
	if got := Repeat("ab", 3.0); got != "ababab" {
		t.Errorf(`Repeat("ab", 3) = %q, want "ababab"`, got)
	}
}

func TestRepeat_ZeroTimesIsEmptyString(t *testing.T) {
	if got := Repeat("ab", 0.0); got != "" {
		t.Errorf(`Repeat("ab", 0) = %q, want ""`, got)
	}
}

func TestRepeat_NegativeCountPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected Repeat(\"ab\", -1) to panic")
		}
	}()
	Repeat("ab", -1.0)
}

func TestToNumber(t *testing.T) {
	if got := ToNumber("42"); got != 42.0 {
		t.Errorf(`ToNumber("42") = %v, want 42`, got)
	}
	if got := ToNumber("3.5"); got != 3.5 {
		t.Errorf(`ToNumber("3.5") = %v, want 3.5`, got)
	}
	if got := ToNumber("-2.25"); got != -2.25 {
		t.Errorf(`ToNumber("-2.25") = %v, want -2.25`, got)
	}
}

func TestToNumber_InvalidStringPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal(`expected ToNumber("not a number") to panic`)
		}
	}()
	ToNumber("not a number")
}
