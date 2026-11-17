"use strict";

// browser.js runs page.js the way a browser would, against a document stubbed
// from the page's own markup and a fetch stubbed from a routing table.
//
// It exists because every other gate on this page is a string match over the
// source, and a string match cannot tell a function that runs from one that is
// declared and never reached. This one observes what the page did: which
// endpoints it asked for, what it wrote into which element, and whether it
// reached for an element the document does not carry.
//
// It is a stub and not a browser. Layout, styling and event dispatch are not
// modelled, so nothing here says anything about how the page looks; what it
// pins is control flow and rendered text.

const fs = require("fs");
const vm = require("vm");

const plan = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));

// The console is several files concatenated into one script, which is how the
// browser gets it: the policy names one block by the hash of its bytes. They are
// joined here in the order the caller gave them, so what runs is what ships.
const source = process.argv.slice(3)
  .map(function (path) { return fs.readFileSync(path, "utf8"); }).join("\n");

const report = {
  fetched: [],
  calls: [],     // the same requests with the method each carried
  missing: [],   // ids the console asked for that the document does not carry
  text: {},
  className: {},
  hidden: {},
  rows: {},
  values: {},
  failures: [],  // rejections nothing on the page handled
  location: "",
};

// ── the document ─────────────────────────────────────────────────────────────

function node(declared) {
  const self = {
    tagName: declared.tag,
    id: declared.id || "",
    children: [],
    className: declared.class || "",
    hidden: declared.hidden === true,
    checked: declared.checked === true,
    // The form controls the console reads back. A field the document declares a
    // value on carries it here, because a page that starts from a default the
    // stub invented is a page tested against a document it does not have.
    value: declared.value || "",
    disabled: false,
    type: "",
    spellcheck: false,
    listeners: {},
    _text: "",

    get textContent() {
      return self._text;
    },
    set textContent(value) {
      self._text = value === undefined || value === null ? "" : String(value);
      self.children.length = 0;
    },

    get firstChild() {
      return self.children.length > 0 ? self.children[0] : null;
    },
    appendChild(child) {
      self.children.push(child);

      return child;
    },
    removeChild(child) {
      const at = self.children.indexOf(child);
      if (at >= 0) { self.children.splice(at, 1); }

      return child;
    },
    addEventListener(name, fn) {
      self.listeners[name] = fn;
    },
  };

  if (declared.tag === "table") { self.tBodies = [node({ tag: "tbody" })]; }

  return self;
}

const elements = new Map();
for (const declared of plan.elements) { elements.set(declared.id, node(declared)); }

const document = {
  getElementById(id) {
    if (!elements.has(id)) {
      report.missing.push(id);

      return null;
    }

    return elements.get(id);
  },
  createElement(tag) {
    return node({ tag: tag });
  },
};

const window = {
  location: { href: plan.url },
  history: {
    replaceState(_state, _title, href) {
      window.location.href = href;
    },
  },
};

// ── the network ──────────────────────────────────────────────────────────────

function route(path) {
  if (Object.hasOwn(plan.routes, path)) { return plan.routes[path]; }

  const cut = path.indexOf("?");
  if (cut >= 0 && Object.hasOwn(plan.routes, path.slice(0, cut))) {
    return plan.routes[path.slice(0, cut)];
  }

  return null;
}

// streamOf turns a route's frames into the reader page.js pulls the live wire
// through. Once the frames are spent the read never settles, which is what an
// open stream with nothing on it does; ending it instead would put the page into
// its reconnect path and report a wire that had never opened.
function streamOf(frames) {
  const encoder = new TextEncoder();
  let at = 0;

  return {
    getReader() {
      return {
        read() {
          if (at >= frames.length) { return new Promise(function () {}); }

          const chunk = encoder.encode(frames[at]);
          at++;

          return Promise.resolve({ done: false, value: chunk });
        },
      };
    },
  };
}

// answer is one response as the console reads it. It carries headers and text
// because every request goes through one wrapper that reads both: an entity-tag
// is the precondition a write quotes back, and the body is parsed once rather
// than through a json method that a refusal has no body for.
function answer(status, text) {
  return {
    ok: status >= 200 && status < 300,
    status: status,
    body: null,
    headers: { get(name) { return name.toLowerCase() === "etag" ? '"v1"' : null; } },
    text() { return Promise.resolve(text || ""); },
    json() { return Promise.resolve(JSON.parse(text || "{}")); },
  };
}

function fetchStub(path, init) {
  const method = (init && init.method) || "GET";

  const headers = (init && init.headers) || {};

  report.fetched.push(path);
  report.calls.push({
    method: method,
    path: path,
    // The preconditions a write carries. The API refuses a write with neither,
    // so a case that could not see them could not tell a console that quotes
    // the version it read from one that overwrites whoever wrote last.
    ifMatch: headers["If-Match"] || "",
    ifNoneMatch: headers["If-None-Match"] || "",
    sent: (init && init.body) || "",
  });

  // A route may be written for one method, which is how a case says that a
  // button sent a PUT and not that something reached the same path.
  const found = route(method + " " + path) || route(path);

  // A request that is accepted and never answered. It is what a proxy in front
  // of a loaded server does, and it is a different failure from a refusal: a
  // page that guards every rejection still waits here for ever.
  if (found !== null && found.hang) { return new Promise(function () {}); }

  if (found === null) {
    // An unrouted path is a 404 and not a thrown request. A page that only
    // survives because the harness was generous is not a page that survives.
    return Promise.resolve(answer(404, ""));
  }

  const resp = answer(found.status || 200, found.body);
  resp.body = found.frames ? streamOf(found.frames) : null;

  return Promise.resolve(resp);
}

// ── running it ───────────────────────────────────────────────────────────────

process.on("unhandledRejection", function (err) {
  report.failures.push(String(err && err.message ? err.message : err));
});
process.on("uncaughtException", function (err) {
  report.failures.push(String(err && err.message ? err.message : err));
});

// The sandbox carries what a browser provides and a bare vm context does not.
// The language's own intrinsics are the new context's, so a page reaching for
// something no browser gives it is a reference error here rather than a pass.
const sandbox = {
  document: document,
  window: window,
  fetch: fetchStub,
  requestAnimationFrame(fn) { return setTimeout(fn, 0); },
  setTimeout: setTimeout,
  clearTimeout: clearTimeout,
  setInterval: setInterval,
  clearInterval: clearInterval,
  console: { error() {}, log() {}, warn() {} },
  URL: URL,
  TextDecoder: TextDecoder,
  TextEncoder: TextEncoder,
};
sandbox.globalThis = sandbox;

vm.createContext(sandbox);

try {
  vm.runInContext(source, sandbox, { filename: "console.js" });
} catch (err) {
  report.failures.push(String(err && err.message ? err.message : err));
}

// settle is how long the page is given to finish what it started. It outlasts
// the page's own coalescing windows, which are a few hundred milliseconds, and
// falls well inside its reconnect delay and its roster clock, which are seconds:
// what the page schedules for later must not fire inside a case that is about
// what it did on load.
const settle = 700;

// press is the second half of a case: the page has loaded, and now somebody uses
// it. A console is mostly buttons, and a button whose handler is never run is
// code no gate here has ever executed.
//
// The fields are filled first and in one go, because a form is filled before it
// is submitted and a case that had to interleave the two would be describing an
// order the page does not require.
setTimeout(function () {
  for (const [id, value] of Object.entries(plan.values || {})) {
    const target = elements.get(id);
    if (target === undefined) { report.missing.push(id); continue; }
    if (typeof value === "boolean") { target.checked = value; } else { target.value = value; }
  }

  for (const id of plan.clicks || []) {
    const target = elements.get(id);

    if (target === undefined || target.listeners.click === undefined) {
      report.failures.push("nothing listens for a click on " + id);
      continue;
    }

    try {
      target.listeners.click();
    } catch (err) {
      report.failures.push(String(err && err.message ? err.message : err));
    }
  }
}, settle);

setTimeout(function () {
  for (const [id, element] of elements) {
    report.text[id] = element.textContent;
    report.className[id] = element.className;
    report.hidden[id] = element.hidden === true;
    report.values[id] = element.value;

    if (element.tBodies) {
      report.rows[id] = element.tBodies[0].children.map(function (row) {
        return row.children.map(function (cellNode) { return cellNode.textContent; });
      });
    }
  }

  report.location = window.location.href;

  process.stdout.write(JSON.stringify(report));
  process.exit(0);
}, settle * 2);
