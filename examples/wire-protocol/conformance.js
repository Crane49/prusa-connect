// Run wire.js against the published conformance vectors.
//
//     node examples/wire-protocol/conformance.js [path to the artefact]
//
// The artefact is docs/wire/plugin-wire-protocol.json, written by
// cmd/generate-wire-protocol. Nothing here reads Go, and this file is what an
// author in another language runs to know their implementation is right.
//
// Exit status is 0 when every vector passed and 1 when any failed.

'use strict';

const fs = require('node:fs');
const path = require('node:path');

const wire = require('./wire.js');

const DEFAULT_ARTEFACT = path.join(__dirname, '..', '..', 'docs', 'wire', 'plugin-wire-protocol.json');

// Report collects what passed and what did not.
class Report {
  constructor() {
    this.passed = 0;
    this.failures = [];
  }

  check(name, condition, detail = '') {
    if (condition) {
      this.passed += 1;
    } else {
      this.failures.push(`${name}: ${detail}`);
    }
  }
}

// canonical renders an object as text with its keys in order, so two objects
// compare equal whatever order their members were written in.
function canonical(object) {
  const sorted = {};

  for (const key of Object.keys(object).sort()) {
    sorted[key] = object[key];
  }

  return JSON.stringify(sorted);
}

// payloadOf builds a payload from hex, or from the repetition rule a boundary
// vector uses instead of spelling out its bytes.
function payloadOf(vector, hexKey, repeatKey) {
  if (hexKey && vector[hexKey]) {
    return Buffer.from(vector[hexKey], 'hex');
  }

  const repeat = vector[repeatKey];

  if (repeat) {
    return Buffer.alloc(repeat.count, repeat.byte);
  }

  return Buffer.alloc(0);
}

// checkSizes holds the constants in wire.js to the published ones.
function checkSizes(artefact, report) {
  const sizes = artefact.sizes;

  const published = [
    ['header_bytes', wire.HEADER_BYTES],
    ['default_max_payload_bytes', wire.DEFAULT_MAX_PAYLOAD_BYTES],
    ['chunk_bytes', wire.CHUNK_BYTES],
    ['max_reset_bytes', wire.MAX_RESET_BYTES],
    ['credit_bytes', wire.CREDIT_BYTES],
    ['max_credit', wire.MAX_CREDIT],
    ['max_handshake_bytes', wire.MAX_HANDSHAKE_BYTES],
  ];

  for (const [name, got] of published) {
    report.check(`size ${name}`, sizes[name] === got, `published ${sizes[name]}, this file holds ${got}`);
  }

  report.check('byte order', sizes.byte_order === 'big-endian', sizes.byte_order);
  report.check(
    'protocol major',
    artefact.protocol.major === wire.PROTOCOL_MAJOR,
    `published ${artefact.protocol.major}`,
  );

  const kinds = {};
  for (const kind of artefact.kinds) {
    kinds[kind.name] = kind.code;
  }

  report.check(
    'the kind set',
    canonical(kinds) === canonical(wire.KIND_CODES),
    `published ${canonical(kinds)}, this file holds ${canonical(wire.KIND_CODES)}`,
  );
}

// checkFrames encodes every frame to the published bytes and decodes it back.
function checkFrames(artefact, report) {
  for (const vector of artefact.frames) {
    const name = vector.name;
    const payload = payloadOf(vector, 'payload_hex', 'payload_repeat');
    const expected = Buffer.concat([Buffer.from(vector.header_hex, 'hex'), payload]);
    const frame = new wire.Frame(vector.stream, wire.KIND_CODES[vector.kind], payload);

    let encoded;

    try {
      encoded = wire.encode(frame);
    } catch (refused) {
      report.check(`encode ${name}`, false, `refused with ${refused.reason}`);

      continue;
    }

    report.check(`encode ${name}`, encoded.equals(expected), `${encoded.length} bytes against ${expected.length}`);

    const reader = new wire.Reader(wire.bufferSource(expected));
    let read;

    try {
      read = reader.next();
    } catch (refused) {
      report.check(`decode ${name}`, false, `refused with ${refused.reason}`);

      continue;
    }

    report.check(`decode ${name}`, read !== null && read.equals(frame), `read ${read}`);
    report.check(`one frame in ${name}`, reader.next() === null, 'more bytes followed the frame');
  }
}

// checkRefusals refuses every refusal vector, for the published reason.
function checkRefusals(artefact, report) {
  for (const vector of artefact.refusals) {
    const name = vector.name;
    const raw = Buffer.concat([Buffer.from(vector.bytes_hex, 'hex'), payloadOf(vector, '', 'bytes_repeat')]);
    const reader = new wire.Reader(wire.bufferSource(raw), vector.max_payload_bytes);

    try {
      reader.next();
    } catch (refused) {
      report.check(
        `refuse ${name}`,
        refused.reason === vector.reason,
        `refused with ${refused.reason}, and the vector states ${vector.reason}`,
      );

      continue;
    }

    report.check(`refuse ${name}`, false, 'accepted');
  }
}

// checkHandshakes reads or refuses every handshake line as published.
function checkHandshakes(artefact, report) {
  for (const vector of artefact.handshakes) {
    const name = vector.name;
    let read;

    try {
      read = wire.parseHandshake(`${vector.line}\n`);
    } catch (refused) {
      if (vector.accepted) {
        report.check(`handshake ${name}`, false, `refused with ${refused.reason}`);
      } else {
        report.check(
          `handshake ${name}`,
          refused.reason === vector.reason,
          `refused with ${refused.reason}, and the vector states ${vector.reason}`,
        );
      }

      continue;
    }

    if (!vector.accepted) {
      report.check(`handshake ${name}`, false, 'accepted');

      continue;
    }

    const stated = {
      protocol: vector.read.protocol,
      min_minor: vector.read.min_minor,
      max_minor: vector.read.max_minor,
      address: vector.read.address,
    };

    report.check(
      `handshake ${name}`,
      canonical(read) === canonical(stated),
      `read ${canonical(read)}, and the vector states ${canonical(stated)}`,
    );
  }
}

// checkNegotiations settles every version pairing as published.
function checkNegotiations(artefact, report) {
  for (const vector of artefact.negotiations) {
    const name = vector.name;
    let minor;

    try {
      minor = wire.negotiate(vector.host, vector.child);
    } catch (refused) {
      if (vector.accepted) {
        report.check(`negotiate ${name}`, false, `refused with ${refused.reason}`);
      } else {
        report.check(
          `negotiate ${name}`,
          refused.reason === vector.reason,
          `refused with ${refused.reason}, and the vector states ${vector.reason}`,
        );
      }

      continue;
    }

    if (!vector.accepted) {
      report.check(`negotiate ${name}`, false, `settled on minor ${minor}`);

      continue;
    }

    report.check(
      `negotiate ${name}`,
      minor === vector.minor,
      `settled on ${minor}, and the vector states ${vector.minor}`,
    );
  }
}

function main() {
  const file = process.argv[2] ?? DEFAULT_ARTEFACT;
  const artefact = JSON.parse(fs.readFileSync(file, 'utf8'));
  const report = new Report();

  checkSizes(artefact, report);
  checkFrames(artefact, report);
  checkRefusals(artefact, report);
  checkHandshakes(artefact, report);
  checkNegotiations(artefact, report);

  // A checker that found no vectors would report a clean run over nothing.
  const sections = ['frames', 'refusals', 'handshakes', 'negotiations'];
  const counted = sections.reduce((total, section) => total + artefact[section].length, 0);

  if (counted < 39) {
    console.log(`the artefact holds ${counted} vectors and this run expects at least 39`);

    return 1;
  }

  for (const failure of report.failures) {
    console.log(`FAIL ${failure}`);
  }

  console.log(`${report.passed} checks passed over ${counted} vectors, ${report.failures.length} failed`);

  return report.failures.length > 0 ? 1 : 0;
}

process.exit(main());
