package main

import (
	"reflect"
	"strings"
	"testing"
)

// Test one continuous string comprised of 140 emoji characters, which should be left as-is.
func TestTruncateEmojiUnchanged(t *testing.T) {
	emojiArray := [4]string{"👾", "🙋🏽", "👨‍🎤", "👨‍👩‍👧‍👦"}
	for _, emoji := range emojiArray {
		longEmojiString := strings.Repeat(emoji, 140)
		got := TruncateTweetBody(longEmojiString)
		if len(got) != 1 || got[0] != longEmojiString {
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
		if got[0] != strings.TrimSuffix(strings.Repeat(emoji+" ", 92), " ")+"..." {
			t.Errorf("Repeated '%s ' 100 times, got %s", emoji, got)
		}
	}
}

// Test one continuous string comprised of 280 ascii characters, which should be left as-is
func TestTruncateTextUnchanged(t *testing.T) {
	charArray := [4]string{"a", "A", ".", "Њ"}
	for _, char := range charArray {
		longTextString := strings.Repeat(char, 280)
		got := TruncateTweetBody(longTextString)
		if len(got) != 1 || got[0] != longTextString {
			t.Errorf("Repeated '%s' 280 times, got %s", char, got)
		}
	}
}

// Test string comprised of 100 copies of ",,,  ", which should be truncated.
func TestTruncateTextShorten(t *testing.T) {
	repeatingString := ",,,  "
	longEmojiString := strings.Repeat(repeatingString, 100)
	got := TruncateTweetBody(longEmojiString)
	if got[0] != strings.TrimSuffix(strings.Repeat(",,, ", 69), " ")+"..." {
		t.Errorf("Repeated '%s' 100 times, got %s", repeatingString, got)
	}
}

func TestComplexWikipediaSamples(t *testing.T) {
	test1 := "A yellow-bellied sapsucker (*Sphyrapicus varius*), a medium-sized woodpecker, perched on a tree in Central Park in New York City, New York, USA. These sapsuckers drill neatly organized rows of holes through which it does not \"suck\" the sap, but uses a brush-shaped tongue to lap it up. The red coloring on its head and throat indicates a male."
	actual1 := TruncateTweetBody(test1)
	test2 := "The red-headed myzomela or red-headed honeyeater (Myzomela erythrocephala) is a passerine bird of the honeyeater family Meliphagidae found in Australia, Indonesia, and Papua New Guinea. It was described by John Gould in 1840. Two subspecies are recognised, with the nominate race M. e. erythrocephala distributed around the tropical coastline of Australia, and M. e. infuscata in New Guinea. Though widely distributed, it is not abundant within this range. While the IUCN lists the Australian population of M. e. infuscata as being near threatened, as a whole the widespread range means that its conservation is of least concern."
	actual2 := TruncateTweetBody(test2)
	test3 := "At 12 cm (4.7 in), it is a small honeyeater with a short tail and relatively long down-curved bill. It is sexually dimorphic; the male has a glossy red head and brown upperparts and paler grey-brown underparts while the female has predominantly grey-brown plumage. Its natural habitat is subtropical or tropical mangrove forests. It is very active when feeding in the tree canopy, darting from flower to flower and gleaning insects off foliage. It calls constantly as it feeds. While little has been documented on the red-headed myzomela's breeding behaviour, it is recorded as building a small cup-shaped nest in the mangroves and laying two or three oval, white eggs with small red blotches."
	actual3 := TruncateTweetBody(test3)

	target1 := []string{"A yellow-bellied sapsucker (*Sphyrapicus varius*), a medium-sized woodpecker, perched on a tree in Central Park in New York City, New York, USA. These sapsuckers drill neatly organized rows of holes through which it does not \"suck\" the sap, but uses a brush-shaped tongue to...",
		"...lap it up. The red coloring on its head and throat indicates a male."}
	target2 := []string{"The red-headed myzomela or red-headed honeyeater (Myzomela erythrocephala) is a passerine bird of the honeyeater family Meliphagidae found in Australia, Indonesia, and Papua New Guinea. It was described by John Gould in 1840. Two subspecies are recognised, with the nominate...",
		"...race M. e. erythrocephala distributed around the tropical coastline of Australia, and M. e. infuscata in New Guinea. Though widely distributed, it is not abundant within this range. While the IUCN lists the Australian population of M. e. infuscata as being near threatened,...",
		"...as a whole the widespread range means that its conservation is of least concern."}
	target3 := []string{"At 12 cm (4.7 in), it is a small honeyeater with a short tail and relatively long down-curved bill. It is sexually dimorphic; the male has a glossy red head and brown upperparts and paler grey-brown underparts while the female has predominantly grey-brown plumage. Its natural...",
		"...habitat is subtropical or tropical mangrove forests. It is very active when feeding in the tree canopy, darting from flower to flower and gleaning insects off foliage. It calls constantly as it feeds. While little has been documented on the red-headed myzomela's breeding...",
		"...behaviour, it is recorded as building a small cup-shaped nest in the mangroves and laying two or three oval, white eggs with small red blotches."}

	if !reflect.DeepEqual(actual1, target1) {
		t.Errorf("test 1 failed, see logs for details")
	}
	if !reflect.DeepEqual(actual2, target2) {
		t.Errorf("test 2 failed, see logs for details")
	}
	if !reflect.DeepEqual(actual3, target3) {
		t.Errorf("test 3 failed, see logs for details")
	}
}
