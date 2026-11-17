// The plugin wire protocol, implemented from the published specification.
//
// The only source for this file was the page
// docs/src/content/docs/plugin-wire-protocol.md. Nobody read the Go
// implementation to write it. It shows an author who uses another language
// how much code the protocol needs, and that the page has enough detail to
// write that code.
//
// It uses the standard library of Node and no npm package.

'use strict';

const HEADER_BYTES = 9;
const DEFAULT_MAX_PAYLOAD_BYTES = 1048576;
const CHUNK_BYTES = 32768;
const MAX_RESET_BYTES = 1024;
const CREDIT_BYTES = 4;
const MAX_CREDIT = 4294967295;
const MAX_HANDSHAKE_BYTES = 4096;

const PROTOCOL_MAJOR = 1;

const KIND_CONTROL = 1;
const KIND_DATA = 2;
const KIND_END = 3;
const KIND_RESET = 4;
const KIND_CREDIT = 5;

const KIND_NAMES = new Map([
  [KIND_CONTROL, 'control'],
  [KIND_DATA, 'data'],
  [KIND_END, 'end'],
  [KIND_RESET, 'reset'],
  [KIND_CREDIT, 'credit'],
]);

const KIND_CODES = {};
for (const [code, name] of KIND_NAMES) {
  KIND_CODES[name] = code;
}

// Refused is the error for input the specification refuses. `reason` is one of
// the reason names the specification publishes, so a caller compares it with
// the reason of a test vector and does not parse the message.
class Refused extends Error {
  constructor(reason, detail = '') {
    super(detail ? `${reason}: ${detail}` : reason);
    this.name = 'Refused';
    this.reason = reason;
  }
}

// Frame is one message on the wire.
class Frame {
  constructor(stream, kind, payload = Buffer.alloc(0)) {
    this.stream = stream;
    this.kind = kind;
    this.payload = payload;
  }

  equals(other) {
    return (
      other instanceof Frame &&
      this.stream === other.stream &&
      this.kind === other.kind &&
      this.payload.equals(other.payload)
    );
  }

  toString() {
    const kind = KIND_NAMES.get(this.kind) ?? this.kind;

    return `Frame(stream=${this.stream}, kind=${kind}, payload=${this.payload.length} bytes)`;
  }
}

// checkPayload throws Refused when a payload breaks the rules of its frame
// kind.
function checkPayload(kind, payload) {
  if (kind === KIND_DATA && payload.length > CHUNK_BYTES) {
    throw new Refused('data_over_the_chunk', `${payload.length} bytes`);
  }

  if (kind === KIND_END && payload.length > 0) {
    throw new Refused('end_carries_a_payload', `${payload.length} bytes`);
  }

  if (kind === KIND_RESET && payload.length > MAX_RESET_BYTES) {
    throw new Refused('reset_over_the_limit', `${payload.length} bytes`);
  }

  if (kind === KIND_CREDIT) {
    if (payload.length !== CREDIT_BYTES) {
      throw new Refused('credit_length', `${payload.length} bytes`);
    }

    if (payload.readUInt32BE(0) === 0) {
      throw new Refused('credit_zero');
    }
  }
}

// encode returns the bytes of one frame, as they are written to the channel.
function encode(frame, maxPayload = DEFAULT_MAX_PAYLOAD_BYTES) {
  if (frame.stream === 0) {
    throw new Refused('stream_zero');
  }

  if (!KIND_NAMES.has(frame.kind)) {
    throw new Refused('unknown_kind', String(frame.kind));
  }

  if (frame.payload.length > maxPayload) {
    throw new Refused('payload_over_the_cap', `${frame.payload.length} bytes`);
  }

  checkPayload(frame.kind, frame.payload);

  const header = Buffer.alloc(HEADER_BYTES);
  header.writeUInt32BE(frame.payload.length, 0);
  header.writeUInt32BE(frame.stream, 4);
  header.writeUInt8(frame.kind, 8);

  return Buffer.concat([header, frame.payload]);
}

// Reader reads frames from a channel.
//
// After one refusal, every later call refuses too. The reader did not parse
// the refused frame, so it does not know where the next frame starts.
//
// The source is an object with a read(size) method. It returns up to `size`
// bytes as a Buffer, or nothing when the channel ended.
class Reader {
  constructor(source, maxPayload = DEFAULT_MAX_PAYLOAD_BYTES) {
    this.source = source;
    this.max = maxPayload;
    this.stopped = false;
  }

  // next reads one frame, or returns null when the channel ended between two.
  next() {
    if (this.stopped) {
      throw new Refused('desynchronised', 'the reader refused an earlier frame');
    }

    const header = this.readExactly(HEADER_BYTES, true);
    if (header === null) {
      return null;
    }

    const size = header.readUInt32BE(0);
    const stream = header.readUInt32BE(4);
    const kind = header.readUInt8(8);

    if (stream === 0) {
      this.stop('stream_zero');
    }

    if (!KIND_NAMES.has(kind)) {
      this.stop('unknown_kind', String(kind));
    }

    if (size > this.max) {
      this.stop('payload_over_the_cap', `${size} bytes against a cap of ${this.max}`);
    }

    const payload = this.readExactly(size, false);

    try {
      checkPayload(kind, payload);
    } catch (refused) {
      this.stopped = true;

      throw refused;
    }

    return new Frame(stream, kind, payload);
  }

  stop(reason, detail = '') {
    this.stopped = true;

    throw new Refused(reason, detail);
  }

  readExactly(size, allowEmpty) {
    const parts = [];
    let got = 0;

    while (got < size) {
      const chunk = this.source.read(size - got);

      if (!chunk || chunk.length === 0) {
        if (got === 0 && allowEmpty) {
          return null;
        }

        this.stopped = true;

        throw new Refused('truncated', `${got} of ${size} bytes`);
      }

      parts.push(chunk);
      got += chunk.length;
    }

    return Buffer.concat(parts, got);
  }
}

// bufferSource wraps a Buffer as a source with the read(size) method that
// Reader needs.
function bufferSource(bytes) {
  let at = 0;

  return {
    read(size) {
      const chunk = bytes.subarray(at, at + size);
      at += chunk.length;

      return chunk;
    },
  };
}

// creditFrame builds a credit frame granting `granted` payload bytes.
function creditFrame(stream, granted) {
  const payload = Buffer.alloc(CREDIT_BYTES);
  payload.writeUInt32BE(granted, 0);

  return new Frame(stream, KIND_CREDIT, payload);
}

// grantOf returns the number of payload bytes a credit frame grants.
function grantOf(frame) {
  if (frame.kind !== KIND_CREDIT) {
    throw new Refused('unknown_kind', 'not a credit frame');
  }

  checkPayload(frame.kind, frame.payload);

  return frame.payload.readUInt32BE(0);
}

function isWholeNumber(value) {
  return typeof value === 'number' && Number.isInteger(value);
}

// parseHandshake reads the one line a child prints before its first frame.
function parseHandshake(line) {
  let text = line.replace(/\n+$/, '').replace(/\r+$/, '');

  if (Buffer.byteLength(text, 'utf8') + 1 > MAX_HANDSHAKE_BYTES) {
    throw new Refused('malformed_line', `${Buffer.byteLength(text, 'utf8') + 1} bytes`);
  }

  if (text.includes('\n') || text.includes('\r')) {
    throw new Refused('malformed_line', 'a line break inside the line');
  }

  let read;

  try {
    read = JSON.parse(text);
  } catch (broken) {
    throw new Refused('malformed_line', broken.message);
  }

  if (read === null || typeof read !== 'object' || Array.isArray(read)) {
    throw new Refused('malformed_line', 'not an object');
  }

  // Ignore a member this version does not define. A child built for a newer
  // minor version then still works with an older host.
  const handshake = {
    protocol: read.protocol ?? 0,
    min_minor: read.min_minor ?? 0,
    max_minor: read.max_minor ?? 0,
    address: read.address ?? '',
  };

  for (const name of ['protocol', 'min_minor', 'max_minor']) {
    if (!isWholeNumber(handshake[name])) {
      throw new Refused('malformed_line', `${name} is not an integer`);
    }
  }

  if (handshake.protocol === 0) {
    throw new Refused('malformed_line', 'protocol 0');
  }

  if (handshake.min_minor > handshake.max_minor) {
    throw new Refused('empty_minor_range');
  }

  const address = handshake.address;

  if (typeof address !== 'string' || address === '') {
    throw new Refused('no_usable_address');
  }

  for (const character of address) {
    const code = character.codePointAt(0);

    if (code < 0x20 || code === 0x7f) {
      throw new Refused('no_usable_address', 'a control character');
    }
  }

  return handshake;
}

// negotiate returns the minor version both peers use, or throws Refused when
// the two cannot work together.
function negotiate(host, child) {
  if (host.protocol !== child.protocol) {
    throw new Refused('different_majors', `${host.protocol} against ${child.protocol}`);
  }

  const low = Math.max(host.min_minor, child.min_minor);
  const high = Math.min(host.max_minor, child.max_minor);

  if (low > high) {
    throw new Refused(
      'no_common_minor',
      `${host.min_minor}-${host.max_minor} against ${child.min_minor}-${child.max_minor}`,
    );
  }

  return high;
}

module.exports = {
  HEADER_BYTES,
  DEFAULT_MAX_PAYLOAD_BYTES,
  CHUNK_BYTES,
  MAX_RESET_BYTES,
  CREDIT_BYTES,
  MAX_CREDIT,
  MAX_HANDSHAKE_BYTES,
  PROTOCOL_MAJOR,
  KIND_CONTROL,
  KIND_DATA,
  KIND_END,
  KIND_RESET,
  KIND_CREDIT,
  KIND_NAMES,
  KIND_CODES,
  Refused,
  Frame,
  Reader,
  bufferSource,
  checkPayload,
  encode,
  creditFrame,
  grantOf,
  parseHandshake,
  negotiate,
};
