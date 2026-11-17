// A plugin child, written from the published specification.
//
// When it runs, it uses the plugin wire protocol on its standard input and
// output: it prints one handshake line and then exchanges frames. It answers
// the lifecycle methods and receives its settings with plugin.configure. It
// also sends a stream of data bytes, and only as many as the host granted as
// credit. A host test uses that to check flow control between two processes
// written in two languages.
//
//     node examples/wire-protocol/child.js
//
// The frame codec is wire.js in this folder, written from the same
// specification page. Both files use the standard library of Node and no npm
// package.
//
// Logs go to standard error. Standard output belongs to the protocol, so this
// file never prints to it.

'use strict';

const fs = require('node:fs');

const wire = require('./wire.js');

const NAME = 'example-blob';
const VERSION = '1.0.0';
const CONTRACT = '1.1.0';

const METHOD_INFO = 'plugin.info';
const METHOD_SHUTDOWN = 'plugin.shutdown';
const METHOD_PING = 'plugin.ping';
const METHOD_CONFIGURE = 'plugin.configure';
const METHOD_READ = 'blob.read';
const METHOD_STARVED = 'blob.starved';

const CODE_METHOD_NOT_FOUND = -32601;
const CODE_INVALID_PARAMS = -32602;

const STDIN = 0;
const STDOUT = 1;

// Job is the data this child still has to send on one stream.
class Job {
  constructor(total, chunk) {
    this.blob = Buffer.alloc(total);

    for (let at = 0; at < total; at += 1) {
      this.blob[at] = at % 251;
    }

    this.chunk = Math.max(1, chunk);
    this.sent = 0;
  }

  left() {
    return this.blob.length - this.sent;
  }
}

// Session is one connection: frames in, frames out, one job per stream.
class Session {
  constructor(source, sink) {
    this.reader = new wire.Reader(source);
    this.sink = sink;
    this.credit = new Map();
    this.jobs = new Map();
    this.stopping = false;
  }

  // run reads frames until the host closes the channel or asks us to stop.
  run() {
    while (!this.stopping) {
      const frame = this.reader.next();

      if (frame === null) {
        return;
      }

      this.receive(frame);
      this.pump();
    }
  }

  // receive acts on one frame.
  receive(frame) {
    if (frame.kind === wire.KIND_CREDIT) {
      const held = this.credit.get(frame.stream) ?? 0;
      this.credit.set(frame.stream, held + wire.grantOf(frame));
    } else if (frame.kind === wire.KIND_CONTROL) {
      this.control(frame.stream, JSON.parse(frame.payload.toString('utf8')));
    } else if (frame.kind === wire.KIND_END || frame.kind === wire.KIND_RESET) {
      this.jobs.delete(frame.stream);
    }
  }

  // control answers one JSON-RPC object.
  control(stream, object) {
    const method = object.method;

    // This is a response to a call. This child makes no calls, so it ignores
    // the response.
    if (method === undefined || method === null) {
      return;
    }

    const identifier = object.id ?? null;

    if (method === METHOD_INFO) {
      this.respond(stream, identifier, {
        name: NAME,
        version: VERSION,
        contract: CONTRACT,
        serves: [METHOD_CONFIGURE],
      });
    } else if (method === METHOD_CONFIGURE) {
      this.configure(stream, identifier, object.params ?? {});
    } else if (method === METHOD_PING) {
      this.respond(stream, identifier, null);
    } else if (method === METHOD_SHUTDOWN) {
      this.respond(stream, identifier, null);
      this.stopping = true;
    } else if (method === METHOD_READ) {
      this.read(stream, identifier, object.params ?? {});
    } else {
      this.fail(stream, identifier, CODE_METHOD_NOT_FOUND, `this child serves no method ${method}`);
    }
  }

  // configure receives the settings. The host built them from the defaults in
  // the manifest of this child and the config block of the operator. This
  // child has one setting, and it fails the call for a key it does not know,
  // which stops the start.
  configure(stream, identifier, params) {
    const held = params.config ?? {};

    for (const key of Object.keys(held)) {
      if (key !== 'greeting') {
        this.fail(stream, identifier, CODE_INVALID_PARAMS, `this child reads no setting ${key}`);
        return;
      }
    }

    this.greeting = held.greeting ?? 'hello';
    this.respond(stream, identifier, null);
  }

  // read handles a request to send `bytes` data bytes on this stream.
  read(stream, identifier, params) {
    const total = params.bytes;

    if (!Number.isInteger(total) || total < 0) {
      this.fail(stream, identifier, CODE_INVALID_PARAMS, 'bytes is a count');

      return;
    }

    this.jobs.set(stream, new Job(total, params.chunk ?? wire.CHUNK_BYTES));
  }

  // pump sends data for every job until the job ends or the credit of its
  // stream is used up.
  pump() {
    for (const [stream, job] of [...this.jobs]) {
      while (job.left() > 0 && (this.credit.get(stream) ?? 0) > 0) {
        const room = Math.min(job.chunk, job.left(), this.credit.get(stream), wire.CHUNK_BYTES);
        this.write(new wire.Frame(stream, wire.KIND_DATA, job.blob.subarray(job.sent, job.sent + room)));
        this.credit.set(stream, this.credit.get(stream) - room);
        job.sent += room;
      }

      if (job.left() > 0) {
        // The credit is used up. Tell the host on a control frame, which needs
        // no credit. The host learns that this child stopped and how many
        // bytes it sent, and does not have to wait for a timeout.
        this.notify(stream, METHOD_STARVED, { sent: job.sent });
      } else {
        this.write(new wire.Frame(stream, wire.KIND_END));
        this.jobs.delete(stream);
      }
    }
  }

  // respond answers a call. A notification carries no id and gets no answer.
  respond(stream, identifier, result) {
    if (identifier === null) {
      return;
    }

    this.send(stream, { jsonrpc: '2.0', id: identifier, result });
  }

  // fail refuses a call.
  fail(stream, identifier, code, message) {
    if (identifier === null) {
      return;
    }

    this.send(stream, { jsonrpc: '2.0', id: identifier, error: { code, message } });
  }

  // notify sends a notification: a call with no id, which the host does not
  // answer.
  notify(stream, method, params) {
    this.send(stream, { jsonrpc: '2.0', method, params });
  }

  // send puts one JSON-RPC object on a control frame.
  send(stream, object) {
    this.write(new wire.Frame(stream, wire.KIND_CONTROL, Buffer.from(JSON.stringify(object), 'utf8')));
  }

  // write puts one frame on the channel.
  write(frame) {
    this.sink.write(wire.encode(frame));
  }
}

// stdinSource reads standard input one block at a time and blocks until bytes
// arrive. A pipe with no bytes ready fails with EAGAIN. That is not the end of
// the channel, so stdinSource reads again.
const stdinSource = {
  read(size) {
    const block = Buffer.alloc(size);

    for (;;) {
      try {
        const got = fs.readSync(STDIN, block, 0, size, null);

        return got === 0 ? null : block.subarray(0, got);
      } catch (failure) {
        if (failure.code === 'EAGAIN') {
          continue;
        }

        if (failure.code === 'EOF') {
          return null;
        }

        throw failure;
      }
    }
  },
};

// stdoutSink writes bytes to standard output and returns after the whole write
// ended, so no frame waits in a buffer of this process while the host waits
// for it.
const stdoutSink = {
  write(bytes) {
    let at = 0;

    while (at < bytes.length) {
      at += fs.writeSync(STDOUT, bytes, at, bytes.length - at);
    }
  },
};

function main() {
  const handshake = { protocol: wire.PROTOCOL_MAJOR, min_minor: 0, max_minor: 0, address: 'stdio' };

  stdoutSink.write(Buffer.from(`${JSON.stringify(handshake)}\n`, 'utf8'));
  process.stderr.write(`${NAME} ${VERSION} is serving on stdio\n`);

  new Session(stdinSource, stdoutSink).run();

  return 0;
}

process.exit(main());
