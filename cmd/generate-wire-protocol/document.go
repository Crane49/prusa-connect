package main

// Document is the shape of docs/wire/plugin-wire-protocol.json. Other
// languages read the file, so the JSON names follow the specification, not Go.
type Document struct {
	// Generated says what wrote the file and that it is not edited by hand.
	Generated string `json:"generated"`
	// Format is the version of this file's shape.
	Format string `json:"format"`
	// Version is the version of the content.
	Version string `json:"version"`
	// Protocol is the wire version this build implements.
	Protocol Protocol `json:"protocol"`
	// Sizes are the fixed numbers of the wire.
	Sizes Sizes `json:"sizes"`
	// Kinds is the closed set of frame kinds.
	Kinds []Kind `json:"kinds"`
	// Reasons is every refusal reason a vector may name.
	Reasons []string `json:"reasons"`
	// Frames are frames every implementation must encode and decode alike.
	Frames []Frame `json:"frames"`
	// Refusals are inputs every implementation must refuse.
	Refusals []Refusal `json:"refusals"`
	// Handshakes are handshake lines a host accepts or refuses.
	Handshakes []HandshakeCase `json:"handshakes"`
	// Negotiations are version range pairs and their outcome.
	Negotiations []Negotiation `json:"negotiations"`
}

// Protocol is the wire version.
type Protocol struct {
	// Major changes when an older peer would misread a frame.
	Major uint32 `json:"major"`
	// MinMinor is the oldest minor this build speaks.
	MinMinor uint32 `json:"min_minor"`
	// MaxMinor is the newest minor this build speaks.
	MaxMinor uint32 `json:"max_minor"`
}

// Sizes are the fixed numbers of the wire.
type Sizes struct {
	// HeaderBytes is the length of the fixed header of every frame.
	HeaderBytes int `json:"header_bytes"`
	// ByteOrder is how the two lengths and the stream id are written.
	ByteOrder string `json:"byte_order"`
	// DefaultMaxPayloadBytes is a reader's cap when its host states none.
	DefaultMaxPayloadBytes int `json:"default_max_payload_bytes"`
	// ChunkBytes is the largest payload of a data frame.
	ChunkBytes int `json:"chunk_bytes"`
	// MaxResetBytes is the largest reason of a reset frame.
	MaxResetBytes int `json:"max_reset_bytes"`
	// CreditBytes is the payload length of every credit frame.
	CreditBytes int `json:"credit_bytes"`
	// MaxCredit is the largest total credit of one stream.
	MaxCredit int64 `json:"max_credit"`
	// MaxHandshakeBytes is the largest handshake line a host reads.
	MaxHandshakeBytes int `json:"max_handshake_bytes"`
}

// Kind is one frame kind.
type Kind struct {
	// Code is the kind byte.
	Code uint8 `json:"code"`
	// Name is the kind's name in the specification.
	Name string `json:"name"`
	// Summary is what the kind is for.
	Summary string `json:"summary"`
	// Payload is what a payload of this kind must be.
	Payload string `json:"payload"`
}

// Repeat is a payload written as one byte repeated, for a vector at a size
// limit.
type Repeat struct {
	// Byte is the byte to repeat.
	Byte byte `json:"byte"`
	// Count is how many times.
	Count int `json:"count"`
}

// Frame is one vector, checked for both encoding and decoding.
type Frame struct {
	// Name identifies the vector.
	Name string `json:"name"`
	// Doc says what the vector checks.
	Doc string `json:"doc"`
	// Stream is the frame's stream id.
	Stream uint32 `json:"stream"`
	// Kind is the frame's kind, by name.
	Kind string `json:"kind"`
	// HeaderHex is the nine header bytes.
	HeaderHex string `json:"header_hex"`
	// PayloadHex is the payload when it is small enough to write out.
	PayloadHex string `json:"payload_hex,omitempty"`
	// PayloadRepeat is the payload when it is one repeated byte.
	PayloadRepeat *Repeat `json:"payload_repeat,omitempty"`
}

// Refusal is input an implementation must refuse, and the reason it gives.
type Refusal struct {
	// Name identifies the vector.
	Name string `json:"name"`
	// Doc says what the vector checks.
	Doc string `json:"doc"`
	// BytesHex is the input to the reader.
	BytesHex string `json:"bytes_hex"`
	// BytesRepeat is appended to BytesHex for a vector past a size limit.
	BytesRepeat *Repeat `json:"bytes_repeat,omitempty"`
	// MaxPayloadBytes is the reader's payload cap for this vector.
	MaxPayloadBytes int `json:"max_payload_bytes"`
	// Reason is why the frame is refused.
	Reason string `json:"reason"`
}

// Handshake is a version range and the address of a child.
type Handshake struct {
	// Protocol is the wire major.
	Protocol uint32 `json:"protocol"`
	// MinMinor is the oldest minor.
	MinMinor uint32 `json:"min_minor"`
	// MaxMinor is the newest minor.
	MaxMinor uint32 `json:"max_minor"`
	// Address is where the host reaches the child.
	Address string `json:"address"`
}

// HandshakeCase is one line a child may write.
type HandshakeCase struct {
	// Name identifies the vector.
	Name string `json:"name"`
	// Doc says what the vector checks.
	Doc string `json:"doc"`
	// Line is the line without its newline.
	Line string `json:"line"`
	// Accepted says whether a host accepts it.
	Accepted bool `json:"accepted"`
	// Read is what a host parses from an accepted line.
	Read *Handshake `json:"read,omitempty"`
	// Reason is why a refused line is refused.
	Reason string `json:"reason,omitempty"`
}

// Negotiation is two version ranges and the version they agree on.
type Negotiation struct {
	// Name identifies the vector.
	Name string `json:"name"`
	// Doc says what the vector checks.
	Doc string `json:"doc"`
	// Host is the range the host implements.
	Host Handshake `json:"host"`
	// Child is the range the child declared.
	Child Handshake `json:"child"`
	// Accepted says whether the two ranges share a version.
	Accepted bool `json:"accepted"`
	// Minor is the version they agree on.
	Minor uint32 `json:"minor"`
	// Reason is why they agree on none.
	Reason string `json:"reason,omitempty"`
}
