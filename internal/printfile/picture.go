package printfile

import (
	"bytes"
	"image"
	_ "image/jpeg" // registers JPEG, so DecodeConfig reads one
	_ "image/png"  // registers PNG, so DecodeConfig reads one
)

// thumbnailFrom makes the picture a caller is served out of the bytes a file
// held, and answers nil for anything it will not serve.
//
// The header is decoded here before the bytes are served. A file says its
// picture is a PNG, and the answer this server writes says image/png, so a
// browser on this server's origin runs whatever is inside. An SVG carrying a
// script is a PNG as far as a file's own words go. Decoding the header proves
// the media type. The width and the height come from it too: the numbers a
// file wrote beside its picture are the file's, and a page has to reserve
// space from the picture's own numbers.
//
// A QOI picture has no decoder in the standard library and is re-encoded as
// PNG on the way, because no browser reads QOI.
func thumbnailFrom(raw []byte, isQOI bool, mediaType string) *Thumbnail {
	if len(raw) == 0 || len(raw) > MaxThumbnailBytes {
		return nil
	}

	if isQOI {
		converted, ok := pngFromQOI(raw)
		if !ok {
			return nil
		}

		raw, mediaType = converted, mediaPNG
	}

	width, height, ok := pictureSize(raw, mediaType)
	if !ok {
		return nil
	}

	return &Thumbnail{Bytes: raw, MediaType: mediaType, Width: width, Height: height}
}

// Picture checks an encoded picture that arrived without its file, such as the
// one a printer reads out of a file on its own storage, and answers nil for
// anything that is not a PNG or a JPEG this package would serve.
func Picture(raw []byte) *Thumbnail {
	if found := thumbnailFrom(raw, false, mediaPNG); found != nil {
		return found
	}

	return thumbnailFrom(raw, false, mediaJPEG)
}

// pictureSize decodes the header of an encoded picture and reports its size. It
// answers false when the bytes are not a picture, when they are a picture of
// another format than the one claimed, and when the picture is larger than this
// package serves.
func pictureSize(raw []byte, want string) (int, int, bool) {
	config, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return 0, 0, false
	}

	if mediaTypeOf(format) != want {
		return 0, 0, false
	}

	if !sizeAllowed(int64(config.Width), int64(config.Height)) {
		return 0, 0, false
	}

	return config.Width, config.Height, true
}

// mediaTypeOf names what image.DecodeConfig recognised. A format this package
// does not serve gets an empty name, which matches nothing a caller asked for.
func mediaTypeOf(format string) string {
	switch format {
	case "png":
		return mediaPNG
	case "jpeg":
		return mediaJPEG
	}

	return ""
}
