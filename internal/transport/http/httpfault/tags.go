package httpfault

import (
	"net/http"

	"github.com/prusa3d/prusa-connect/sdk/fault"
)

// The param names the tag codes interpolate.
const (
	paramNameTag             = "tag"
	paramNameLimitCharacters = "limit_characters"
	paramNameLimitTags       = "limit_tags"
)

// The codes a tag write or a tag filter returns. Each one names the rule the
// tag broke, so a screen can say what to change in the person's own language.
var (
	tagEmpty = fault.Register(
		fault.DomainValidation(), "TAG_EMPTY", http.StatusUnprocessableEntity, fault.Document(
			"A tag has no visible character",
			"Remove the empty tag, or type a word or a short phrase into it.",
			fault.RetryNever,
			fault.ShowDetail,
			paramNameLocation,
		))

	tagTooLong = fault.Register(
		fault.DomainValidation(), "TAG_TOO_LONG", http.StatusUnprocessableEntity, fault.Document(
			"A tag is longer than this server accepts",
			"Shorten the tag named by tag to at most limit_characters characters.",
			fault.RetryNever,
			fault.ShowDetail,
			paramNameLocation, paramNameTag, paramNameLimitCharacters,
		))

	tagCharacter = fault.Register(
		fault.DomainValidation(), "TAG_CHARACTER", http.StatusUnprocessableEntity, fault.Document(
			"A tag contains a character this server does not accept",
			"Remove any comma, line break, tab or character that does not show on screen. "+
				"An accent or a part of an emoji must come right after a letter, a digit or a symbol, "+
				"and not at the start of a word. The device list uses commas to separate tags.",
			fault.RetryNever,
			fault.ShowDetail,
			paramNameLocation, paramNameTag,
		))

	tagDuplicate = fault.Register(
		fault.DomainValidation(), "TAG_DUPLICATE", http.StatusUnprocessableEntity, fault.Document(
			"The same tag is given twice",
			"Send each tag once. Two tags are the same tag when they differ only in upper and lower case, "+
				"or in the invisible characters that join letters or pick how an emoji looks.",
			fault.RetryNever,
			fault.ShowDetail,
			paramNameLocation, paramNameTag,
		))

	tagsTooMany = fault.Register(
		fault.DomainValidation(), "TAGS_TOO_MANY", http.StatusUnprocessableEntity, fault.Document(
			"A device is given more tags than this server accepts",
			"Send at most limit_tags tags for one device.",
			fault.RetryNever,
			fault.ShowDetail,
			paramNameLocation, paramNameLimitTags,
		))
)

// TagEmpty reports a tag with no visible character once its spaces are
// trimmed. location is the member, such as body.tags[2].
func TagEmpty(location string) *fault.Fault {
	return tagEmpty.New("a tag has no visible character").With(fault.StringParam(paramNameLocation, location))
}

// TagTooLong reports a tag over the length limit, which is counted in
// characters.
func TagTooLong(location, tag string, limit int) *fault.Fault {
	return tagTooLong.New("a tag is too long").With(
		fault.StringParam(paramNameLocation, location),
		fault.StringParam(paramNameTag, tag),
		fault.IntParam(paramNameLimitCharacters, int64(limit)),
	)
}

// TagCharacter reports a tag carrying a comma, a control character or a
// character that shows nothing, or a joiner, selector or accent out of place.
func TagCharacter(location, tag string) *fault.Fault {
	return tagCharacter.New("a tag contains a character that is not accepted").With(
		fault.StringParam(paramNameLocation, location),
		fault.StringParam(paramNameTag, tag),
	)
}

// TagDuplicate reports a tag given twice in one list. Case, joiners and
// presentation selectors are ignored in the comparison.
func TagDuplicate(location, tag string) *fault.Fault {
	return tagDuplicate.New("a tag is given twice").With(
		fault.StringParam(paramNameLocation, location),
		fault.StringParam(paramNameTag, tag),
	)
}

// TagsTooMany reports a list longer than one device may carry.
func TagsTooMany(location string, limit int) *fault.Fault {
	return tagsTooMany.New("too many tags").With(
		fault.StringParam(paramNameLocation, location),
		fault.IntParam(paramNameLimitTags, int64(limit)),
	)
}

// CodeTagEmpty names the code for an empty tag.
func CodeTagEmpty() fault.Code { return tagEmpty.Code() }

// CodeTagTooLong names the code for a tag over the length limit.
func CodeTagTooLong() fault.Code { return tagTooLong.Code() }

// CodeTagCharacter names the code for a tag carrying a refused character.
func CodeTagCharacter() fault.Code { return tagCharacter.Code() }

// CodeTagDuplicate names the code for a tag given twice.
func CodeTagDuplicate() fault.Code { return tagDuplicate.Code() }

// CodeTagsTooMany names the code for a list longer than the limit.
func CodeTagsTooMany() fault.Code { return tagsTooMany.Code() }
