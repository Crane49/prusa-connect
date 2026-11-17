package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
)

// The upload samples. A file upload is the one operation whose request body is
// not JSON, so its sample is a short gcode file written out in full: a reader
// who uploads exactly these bytes gets exactly this answer, digest included.
// The path is the sample's own query, because the answer repeats it.

// exampleUploadPath is where the sample puts the file. It is a plain gcode
// file rather than the binary bracket.bgcode the other storage samples browse,
// because the body is shown as text and a reader can type it.
const exampleUploadPath = "models/warmup.gcode"

// exampleUpload is the file the sample sends: warm the nozzle and the bed for
// PLA, the same lines the preset sample stores.
const exampleUpload = "M104 S215\nM140 S60\nG28\n"

// storageObjectExamples are the worked examples for the two upload operations,
// which are one upload published under PUT and under POST.
func storageObjectExamples() map[string]operationExample {
	upload := operationExample{
		Query:        "path=" + exampleUploadPath,
		RequestMedia: "application/octet-stream",
		Request:      exampleUpload,
		Response: map[string]any{
			"path":   exampleUploadPath,
			"bytes":  len(exampleUpload),
			"sha256": digestOf(exampleUpload),
		},
	}

	return map[string]operationExample{
		"upload-storage-object": upload,
		"post-storage-object":   upload,
	}
}

// digestOf is the SHA-256 of the text, lowercase hex, which is how a storage
// reports the digest of what it wrote.
func digestOf(text string) string {
	sum := sha256.Sum256([]byte(text))

	return hex.EncodeToString(sum[:])
}
