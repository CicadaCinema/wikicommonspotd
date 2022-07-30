package main

import (
	"strings"
	"testing"
)

// Test one continuous string comprised of 140 emoji characters, which should be left as-is.
func TestTruncateEmojiUnchanged(t *testing.T) {
	emojiArray := [4]string{"👾", "🙋🏽", "👨‍🎤", "👨‍👩‍👧‍👦"}
	for _, emoji := range emojiArray {
		longEmojiString := strings.Repeat(emoji, 140)
		got := TruncateTweetBody(longEmojiString)
		if got != longEmojiString {
			t.Errorf("Repeated '%s' 140 times, got %s", emoji, got)
		}
	}
}

// Test string comprised of 100 copies of "e " where e is an emoji, which should be truncated.
func TestTruncateEmojiShorten(t *testing.T) {
	emojiArray := [4]string{"👾", "🙋🏽", "👨‍🎤", "👨‍👩‍👧‍👦"}
	for _, emoji := range emojiArray {
		longEmojiString := strings.Repeat(emoji+" ", 100)
		got := TruncateTweetBody(longEmojiString)
		if got != strings.Repeat(emoji+" ", 92)+"..." {
			t.Errorf("Repeated '%s ' 100 times, got %s", emoji, got)
		}
	}
}

// Test one continuous string comprised of 280 ascii characters, which should be left as-is
func TestTruncateTextUnchanged(t *testing.T) {
	charArray := [4]string{"a", "A", ".", " "}
	for _, char := range charArray {
		longTextString := strings.Repeat(char, 280)
		got := TruncateTweetBody(longTextString)
		if got != longTextString {
			t.Errorf("Repeated '%s' 280 times, got %s", char, got)
		}
	}
}

// Test string comprised of 100 copies of ",,,  ", which should be truncated.
func TestTruncateTextShorten(t *testing.T) {
	repeatingString := ",,,  "
	longEmojiString := strings.Repeat(repeatingString, 100)
	got := TruncateTweetBody(longEmojiString)
	if got != strings.Repeat(",,, ", 69)+"..." {
		t.Errorf("Repeated '%s' 100 times, got %s", repeatingString, got)
	}
}
