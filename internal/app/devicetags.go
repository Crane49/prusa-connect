package app

// Tags on a device. A tag is a plain word or short phrase an operator puts on a
// printer, such as "workshop", "PETG only" or "night shift", to filter and
// group the fleet. It has no key and no value.
//
// Nothing in this file reaches the authorization graph with a tag, and nothing
// in the graph reads one. So a tag narrows a listing the caller may already
// read and can never widen one.

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/prusa3d/prusa-connect/internal/authz"
	"github.com/prusa3d/prusa-connect/internal/transport/http/httpfault"
)

const (
	// maxTagsPerDevice bounds one device's list. A write replaces the whole
	// list, so this is what keeps a caller from storing a dictionary on a
	// device.
	maxTagsPerDevice = 32

	// maxTagRunes bounds one tag in characters and not in bytes, so a tag in
	// Czech or in Japanese is as long as one in English.
	maxTagRunes = 64

	// maxStoredTagRunes is the longest tag the upgrade from labels can store.
	// A label key was a prefix, a slash and a name, and the upgrade joined the
	// key and the value with ": ". A device keeps such a tag, and the listing
	// filter accepts one this long.
	maxStoredTagRunes = maxLabelPrefix + len(labelPrefixSeparator) + maxLabelName +
		len(labelValueSeparator) + maxLabelValue

	// maxTagFilter bounds the tags one listing request may name. The listing
	// parameter publishes the same number.
	maxTagFilter = 16

	// tagSeparator is refused inside a tag, because the listing filter
	// separates tags with it.
	tagSeparator = ","
)

// SetTags replaces the whole tag list on one device and returns what it now
// carries.
//
// It replaces rather than merges, so a client takes a tag off by leaving it
// out. It takes administer on the device, because a tag is an administrative fact
// about a machine and not something the machine did.
func (d *devices) SetTags(ctx context.Context, principal string, id string, tags []string) ([]string, error) {
	cleaned, err := normaliseTags("body.tags", tags)
	if err != nil {
		return nil, err
	}

	return d.writeTags(ctx, principal, id, "body.tags", cleaned)
}

// writeTags stores a list that has passed every rule but the length rule. The
// length rule needs the device's own list, so it is checked after the
// permission. A caller who may not see the device learns nothing from it.
func (d *devices) writeTags(
	ctx context.Context, principal string, id string, location string, tags []string,
) ([]string, error) {
	entry, _, err := d.reachHolding(ctx, principal, id, authz.ActionAdminister)
	if err != nil {
		return nil, err
	}

	held, err := entry.setTags(location, tags)
	if err != nil {
		return nil, err
	}

	// A person typed the list, so it is written and reported at once rather
	// than on the next telemetry flush.
	d.writeSoon(entry)

	return held, nil
}

// setTags writes the list onto the entry and marks the record behind. It
// returns a copy, so the caller holds nothing the entry's mutex guards.
//
// A tag over the length limit is written only when the device already carries
// it with exactly the same text. The upgrade from labels stored such tags, and
// a screen sends the whole list back when a person adds one tag. Sameness is
// not enough here, because a same tag can read differently.
func (e *device) setTags(location string, tags []string) ([]string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	for index, tag := range tags {
		if utf8.RuneCountInString(tag) > maxTagRunes && !slices.Contains(e.tags, tag) {
			return nil, httpfault.TagTooLong(fmt.Sprintf("%s[%d]", location, index), tag, maxTagRunes)
		}
	}

	e.tags = slices.Clone(tags)
	e.changed = true

	return nonNilTags(e.tags), nil
}

// tagList is what this device carries, as a copy. An empty list is an empty
// slice and never nil, so it encodes as [] and a client needs no null check.
func (e *device) tagList() []string {
	e.mu.Lock()
	defer e.mu.Unlock()

	return nonNilTags(e.tags)
}

// nonNilTags copies a list, turning nil into an empty list.
func nonNilTags(tags []string) []string {
	out := make([]string, len(tags))
	copy(out, tags)

	return out
}

// carriesTags reports whether this device carries every tag the filter names.
// The comparison is sameTag's, so "petg only" finds "PETG only". A filter that
// names nothing matches every device.
func (e *device) carriesTags(wanted []string) bool {
	if len(wanted) == 0 {
		return true
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	for _, tag := range wanted {
		if !containsTag(e.tags, tag) {
			return false
		}
	}

	return true
}

// containsTag reports whether the list holds the same tag.
func containsTag(tags []string, tag string) bool {
	return slices.ContainsFunc(tags, func(held string) bool {
		return sameTag(held, tag)
	})
}

// sameTag reports whether two tags are the same tag. It ignores case and the
// joiners and presentation selectors, because a person cannot tell two tags
// apart by them. The stored text keeps them as they were typed.
func sameTag(a, b string) bool {
	return strings.EqualFold(withoutJoiners(a), withoutJoiners(b))
}

// withoutJoiners is a tag with every joiner and presentation selector removed.
func withoutJoiners(tag string) string {
	return strings.Map(func(char rune) rune {
		if isJoiner(char) {
			return -1
		}

		return char
	}, tag)
}

// parseTagFilter reads the listing's tag parameter. Each value is cleaned the
// way a written tag is, and an empty value is dropped, as the state and model
// filters drop one. A value that could never be a tag is refused, so a typo
// does not read as an empty fleet. A value may be as long as a tag the upgrade
// from labels stored.
func parseTagFilter(raw []string) ([]string, error) {
	out := make([]string, 0, len(raw))

	for index, value := range raw {
		if strings.TrimSpace(value) == "" {
			continue
		}

		location := fmt.Sprintf("query.tag[%d]", index)

		tag, err := cleanTag(location, value)
		if err != nil {
			return nil, err
		}

		if utf8.RuneCountInString(tag) > maxStoredTagRunes {
			return nil, httpfault.TagTooLong(location, tag, maxStoredTagRunes)
		}

		out = append(out, tag)
	}

	if len(out) > maxTagFilter {
		return nil, httpfault.TagsTooMany("query.tag", maxTagFilter)
	}

	return out, nil
}

// normaliseTags holds a whole list to the rules before anything is written,
// and returns the cleaned list in the order it was given. A tag may be as long
// as a tag the upgrade from labels stored. The write then refuses it unless the
// device already carries it.
func normaliseTags(location string, tags []string) ([]string, error) {
	if len(tags) > maxTagsPerDevice {
		return nil, httpfault.TagsTooMany(location, maxTagsPerDevice)
	}

	out := make([]string, 0, len(tags))

	for index, raw := range tags {
		member := fmt.Sprintf("%s[%d]", location, index)

		tag, err := cleanTag(member, raw)
		if err != nil {
			return nil, err
		}

		if utf8.RuneCountInString(tag) > maxStoredTagRunes {
			return nil, httpfault.TagTooLong(member, tag, maxTagRunes)
		}

		// Two tags that are the same tag would show as two chips a person
		// cannot tell apart in a filter, so the second one is refused.
		if containsTag(out, tag) {
			return nil, httpfault.TagDuplicate(member, tag)
		}

		out = append(out, tag)
	}

	return out, nil
}

// cleanTag cleans one tag and checks it against every rule but the length.
//
// The tag is trimmed, and every run of spaces inside it becomes one space,
// because a person cannot see the difference. The case is kept as typed. A
// control character is refused, because a tag reaches event payloads and log
// lines. An invisible character is refused, because it can make two different
// tags look the same. A joiner, a presentation selector and an accent are
// accepted where they can show, because emoji and many scripts need them. A
// tag with no visible character counts as empty. A comma is refused, because
// the listing filter separates tags with commas.
func cleanTag(location, raw string) (string, error) {
	if !utf8.ValidString(raw) {
		return "", httpfault.TagCharacter(location, shownTag(raw))
	}

	for _, char := range raw {
		if refusedTagCharacter(char) {
			return "", httpfault.TagCharacter(location, shownTag(raw))
		}
	}

	tag := strings.Join(strings.Fields(raw), " ")

	if !strings.ContainsFunc(tag, visibleTagCharacter) {
		return "", httpfault.TagEmpty(location)
	}

	if !joinersStandRight(tag) {
		return "", httpfault.TagCharacter(location, tag)
	}

	if strings.Contains(tag, tagSeparator) {
		return "", httpfault.TagCharacter(location, tag)
	}

	return tag, nil
}

// blankBraille is the braille pattern with no dots. It is a symbol to Unicode,
// but it shows as an empty space.
const blankBraille = '\u2800'

// showsNothing reports a character that shows nothing and is not marked as a
// formatting character. A variation selector changes how the character before
// it looks. Unicode lists the other characters as ignorable, and a screen
// shows nothing for them. The Hangul fillers are among them.
func showsNothing(char rune) bool {
	return char == blankBraille ||
		unicode.In(char, unicode.Variation_Selector, unicode.Other_Default_Ignorable_Code_Point)
}

// Joiners and presentation selectors. A joiner builds an emoji from others or
// shapes a letter in scripts such as Persian. A presentation selector asks for
// an emoji or a text picture of the character before it.
const (
	zeroWidthNonJoiner = '\u200c'
	zeroWidthJoiner    = '\u200d'
	textPresentation   = '\ufe0e'
	emojiPresentation  = '\ufe0f'
)

// isJoiner reports a joiner or a presentation selector.
func isJoiner(char rune) bool {
	switch char {
	case zeroWidthNonJoiner, zeroWidthJoiner, textPresentation, emojiPresentation:
		return true
	}

	return false
}

// tagPlace is what came just before a character. The rules on where a joiner,
// a presentation selector and a combining mark may stand read it.
type tagPlace int

const (
	// placeStart is the start of the tag, or the place just after a space.
	placeStart tagPlace = iota
	// placeVisible is just after a visible character and any accents on it.
	placeVisible
	// placeSelected is just after a presentation selector on a visible
	// character.
	placeSelected
	// placeJoined is just after a joiner, which needs a visible character next.
	placeJoined
	// placeOther is just after a character that is allowed and is not visible.
	placeOther
	// placeFlag is just after the black flag, which may start a region flag.
	placeFlag
	// placeFlagLetters is inside the letters of a region flag, which must end
	// with the cancel tag.
	placeFlagLetters
)

// A region flag, such as the flag of England, is the black flag followed by
// tag letters that name the region and a cancel tag that ends them.
const (
	blackFlag       = '\U0001F3F4'
	firstFlagLetter = '\U000E0020'
	lastFlagLetter  = '\U000E007E'
	flagCancelTag   = '\U000E007F'
)

// isFlagLetter reports a tag letter or the cancel tag of a region flag.
func isFlagLetter(char rune) bool {
	return char >= firstFlagLetter && char <= flagCancelTag
}

// visiblePlace reports a place just after a visible character.
func visiblePlace(place tagPlace) bool {
	return place == placeVisible || place == placeFlag
}

// joinersStandRight reports whether every joiner, presentation selector and
// combining mark in the tag stands where it can show. A joiner stands between
// two visible characters. A presentation selector comes right after a visible
// character. An accent sits on a visible character. So nothing of these can
// start a word, and two of the joiners and selectors never stand side by side.
func joinersStandRight(tag string) bool {
	place := placeStart

	for _, char := range tag {
		next, allowed := nextTagPlace(place, char)
		if !allowed {
			return false
		}

		place = next
	}

	return place != placeJoined && place != placeFlagLetters
}

// nextTagPlace reports whether the character may stand after the place, and
// the place after it.
func nextTagPlace(place tagPlace, char rune) (tagPlace, bool) {
	switch {
	case place == placeJoined && !visibleTagCharacter(char):
		return place, false
	case char >= firstFlagLetter && char <= lastFlagLetter:
		return placeFlagLetters, place == placeFlag || place == placeFlagLetters
	case char == flagCancelTag:
		return placeVisible, place == placeFlagLetters
	case place == placeFlagLetters:
		return place, false
	case char == ' ':
		return placeStart, true
	case char == zeroWidthJoiner || char == zeroWidthNonJoiner:
		return placeJoined, visiblePlace(place) || place == placeSelected
	case char == textPresentation || char == emojiPresentation:
		return placeSelected, visiblePlace(place)
	case unicode.Is(unicode.M, char):
		// A keycap emoji puts its enclosing mark after the selector.
		return placeVisible, visiblePlace(place) || place == placeSelected
	case char == blackFlag:
		return placeFlag, true
	case visibleTagCharacter(char):
		return placeVisible, true
	}

	return placeOther, true
}

// refusedTagCharacter reports a control character, a formatting character or
// another character that shows nothing. A joiner, a presentation selector and
// the letters of a region flag are checked by their place in the tag instead.
func refusedTagCharacter(char rune) bool {
	if isJoiner(char) || isFlagLetter(char) {
		return false
	}

	return unicode.IsControl(char) || unicode.Is(unicode.Cf, char) || showsNothing(char)
}

// visibleTagCharacter reports a letter, a digit, a punctuation mark or a
// symbol. A combining accent alone is not one, because it needs a letter to
// sit on.
func visibleTagCharacter(char rune) bool {
	return unicode.In(char, unicode.L, unicode.N, unicode.P, unicode.S)
}

// shownTag is a refused tag as the error repeats it. Every refused character
// becomes the replacement character, so the error cannot carry a line break
// into a log or reverse the text on a screen.
func shownTag(raw string) string {
	return strings.Map(func(char rune) rune {
		if refusedTagCharacter(char) {
			return utf8.RuneError
		}

		return char
	}, strings.ToValidUTF8(raw, string(utf8.RuneError)))
}

// renderTags is one list as a single comparable value, so the record frame
// can tell a list that moved from one that did not. Order counts, because the
// order is the operator's.
func renderTags(tags []string) string {
	return strings.Join(tags, "\x00")
}
