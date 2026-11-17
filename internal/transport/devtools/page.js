"use strict";

// The developer console. Everything it renders comes from the ordinary API, so
// the page holds no authority: what a caller may see through /api/v1 is exactly
// what appears here, and every request it makes is on the log at the foot of
// the page.
//
// This file holds the shared helpers, the request log and the printers area.
// The other four areas are files of their own, concatenated after this one into
// the single script the policy names by hash.
//
// The live wire is the event stream read frame by frame rather than through
// EventSource, because EventSource delivers a named event only to a listener
// registered for that name and the point of the events area is the frame nobody
// expected.

const RECONNECT_DELAY = 2000;
const DETAIL_INTERVAL = 1000;

// CALL_LIMIT is how many requests the log at the foot of the page keeps. It is
// the console's own memory and not the server's, so it costs nothing to hold
// enough of a session to see what a screen did before it went wrong.
const CALL_LIMIT = 300;

// ROSTER_COALESCE is how long an arrival waits before the roster is re-read.
//
// An arrival is the one frame that does not carry what the page needs, so it has
// to ask. A server coming back up is the whole fleet arriving inside a second,
// and at this project's stated scale that is four hundred listing requests
// aimed at exactly the machine that is recovering. The window is trailing, so a
// burst costs one request and the printer that arrived last is in it.
const ROSTER_COALESCE = 250;

// ROSTER_PAGE is deliberately above any page size the server will grant.
//
// The listing answers a limit above its cap with the cap and reports which limit
// it applied, so asking large is how a client asks for as much of the fleet as
// it is allowed rather than restating a bound that lives on the server.
const ROSTER_PAGE = 1000;

const TELEMETRY_TYPE = "com.prusa.connect.device.telemetry.sampled";
const ARRIVED_TYPE = "com.prusa.connect.device.arrived";
const DEPARTED_TYPE = "com.prusa.connect.device.departed";
const PRESENCE_TYPE = "com.prusa.connect.device.presence.changed";
const LINK_TYPE = "com.prusa.connect.device.link.changed";

const state = {
  printers: new Map(),   // id -> {view}
  selected: null,
  seen: new Map(),       // printer id -> Map(path -> Date)
  calls: [],
  // capabilities is the capability vocabulary, keyed by verb, so the command
  // form knows what arguments a verb takes without asking a second time.
  capabilities: new Map(),
  // commandFields are the inputs the command form generated, kept by argument
  // name. They are held rather than looked up by id, because a field this page
  // made is a field only this page knows the name of, and an id lookup would put
  // the console's own generated names in the same space as the document's.
  commandFields: new Map(),
};

// gone reports whether a printer's session has ended. It reads the listing's own
// member rather than a flag this page keeps beside it: the departure frame and
// the listing say the same thing, and two places to write it is two answers.
function gone(entry) { return !!(entry && entry.view && entry.view.departed); }

// goneReason is the server's word for why the session ended. It is a closed set
// the API publishes, so it is shown as it arrives.
function goneReason(entry) { return (gone(entry) && entry.view.departed.reason) || ""; }

// ── the document ─────────────────────────────────────────────────────────────

function el(id) { return document.getElementById(id); }

function cell(row, text, className) {
  const td = document.createElement("td");
  td.textContent = text === undefined || text === null ? "" : String(text);
  if (className) { td.className = className; }
  row.appendChild(td);
  return td;
}

// action puts a button inside a cell, which is how a table row offers the one
// thing a reader wants to do to the row they are looking at.
function action(row, label, run) {
  const td = document.createElement("td");
  const button = document.createElement("button");
  button.type = "button";
  button.textContent = label;
  button.addEventListener("click", run);
  td.appendChild(button);
  row.appendChild(td);
  return button;
}

function clear(node) { while (node.firstChild) { node.removeChild(node.firstChild); } }

function body(id) { return el(id).tBodies[0]; }

// say writes one panel's outcome line. Every action on this page ends in one, so
// a button that did nothing and a button that was refused never read alike.
function say(id, text, className) {
  const node = el(id);
  node.textContent = text;
  node.className = "note " + (className || "");
}

// fill replaces a select's options and keeps the selection where the value is
// still on the list, so a list re-read under a reader does not move what they
// had chosen.
function fill(id, values, blank) {
  const node = el(id);
  const wanted = node.value;

  clear(node);

  if (blank !== undefined) {
    const empty = document.createElement("option");
    empty.value = "";
    empty.textContent = blank;
    node.appendChild(empty);
  }

  for (const value of values) {
    const choice = document.createElement("option");
    choice.value = typeof value === "string" ? value : value.value;
    choice.textContent = typeof value === "string" ? value : value.label;
    node.appendChild(choice);
  }

  node.value = values.some(function (value) {
    return (typeof value === "string" ? value : value.value) === wanted;
  }) ? wanted : (blank !== undefined ? "" : node.value);
}

function time(value) {
  const at = value instanceof Date ? value : new Date(value);
  if (isNaN(at.getTime())) { return ""; }
  return at.toISOString().slice(11, 23);
}

function short(value, limit) {
  const text = typeof value === "string" ? value : JSON.stringify(value);
  if (text === undefined) { return ""; }
  return text.length > limit ? text.slice(0, limit) + "…" : text;
}

// lines splits a textarea into the non-empty lines it holds, which is the shape
// every list this console sends is written in.
function lines(text) {
  return String(text || "").split("\n").map(function (line) { return line.trim(); })
    .filter(function (line) { return line !== ""; });
}

function commas(text) {
  return String(text || "").split(",").map(function (part) { return part.trim(); })
    .filter(function (part) { return part !== ""; });
}

// ── the request log ──────────────────────────────────────────────────────────

// problemText renders an application/problem+json body the way the person who
// pressed the button needs it: the registered code first, because that is the
// thing they will search the error reference for, then the server's own words.
function problemText(payload) {
  if (payload === null || payload === undefined) { return ""; }
  if (typeof payload !== "object") { return short(payload, 300); }

  const parts = [];
  if (payload.code) { parts.push(payload.code); }
  if (payload.detail) { parts.push(payload.detail); }
  else if (payload.title) { parts.push(payload.title); }
  if (payload.params && Object.keys(payload.params).length > 0) {
    parts.push(JSON.stringify(payload.params));
  }

  return parts.length > 0 ? parts.join(": ") : short(payload, 300);
}

function recordCall(answer) {
  state.calls.unshift(answer);
  if (state.calls.length > CALL_LIMIT) { state.calls.length = CALL_LIMIT; }
  renderCalls();
}

function renderCalls() {
  const rows = body("calls");
  clear(rows);

  for (const call of state.calls) {
    const row = document.createElement("tr");
    if (!call.ok) { row.className = "failed"; }
    cell(row, time(call.at));
    cell(row, call.method);
    cell(row, call.path);
    cell(row, call.status || "no answer", call.ok ? "ok" : "bad");
    cell(row, call.elapsed);
    cell(row, call.detail);
    rows.appendChild(row);
  }
}

// apiCall is the one way this page reaches the server.
//
// It is one function rather than one per panel so that the log below the page is
// the whole truth about what the console did. A panel that reached the network
// another way would be a request nobody could see, which is the state this
// console exists to end.
async function apiCall(method, path, options) {
  const opts = options || {};
  const started = Date.now();

  const init = { method: method, headers: { Accept: "application/json" } };

  if (opts.body !== undefined) {
    init.headers["Content-Type"] = "application/json";
    init.body = JSON.stringify(opts.body);
  }

  if (opts.ifMatch) { init.headers["If-Match"] = opts.ifMatch; }
  if (opts.ifNoneMatch) { init.headers["If-None-Match"] = opts.ifNoneMatch; }

  const answer = {
    at: new Date(), method: method, path: path,
    status: 0, ok: false, body: null, detail: "", version: "", elapsed: 0,
  };

  try {
    const resp = await fetch(path, init);
    answer.status = resp.status;
    answer.ok = resp.ok;
    answer.version = tagOf(resp);

    const text = await resp.text();
    if (text !== "") {
      try { answer.body = JSON.parse(text); } catch (err) { answer.body = text; }
    }

    if (!resp.ok) { answer.detail = problemText(answer.body); }
  } catch (err) {
    answer.detail = err.message;
  }

  answer.elapsed = Date.now() - started;
  recordCall(answer);

  return answer;
}

// tagOf reads the entity-tag a listing was produced at, which is the fencing
// token a write quotes back. A response with no headers object is a stub or a
// failure, and neither carries one.
function tagOf(resp) {
  if (!resp.headers || typeof resp.headers.get !== "function") { return ""; }
  return resp.headers.get("ETag") || "";
}

// readJSON is a GET whose answer the caller needs. It throws where a panel is
// filled or not filled at all; a panel offering an action calls apiCall and
// reports the refusal instead.
async function readJSON(path) {
  const answer = await apiCall("GET", path);

  if (!answer.ok) {
    throw new Error(path + " answered " + answer.status + (answer.detail ? ": " + answer.detail : ""));
  }

  return answer.body;
}

// ── connectors and the origin's routes ───────────────────────────────────────

async function loadConnectors() {
  const inventory = await readJSON("/api/v1/dev/connectors");

  const connectors = body("connectors");
  clear(connectors);
  const instances = [];
  for (const c of inventory.connectors || []) {
    const row = document.createElement("tr");
    cell(row, c.name);
    cell(row, c.qualified);
    cell(row, c.direction);
    cell(row, c.failure ? c.status + ": " + c.failure : c.status,
      c.status === "mounted" ? "ok" : c.status === "failed" ? "bad" : "warn");
    cell(row, (c.routes || []).join("  "));
    cell(row, c.baseline_capabilities);
    connectors.appendChild(row);
    instances.push(c.name);
  }

  // The endpoint forms name a connector instance, and the set of them is this
  // inventory. Typing one is how a reader declares an endpoint against a
  // dialect this build does not carry and learns so from a 422.
  fill("endpoint-connector", instances, "pick one");
  fill("setup-printer-connector", instances, "pick one");

  const routes = body("routes");
  clear(routes);
  for (const r of inventory.routes || []) {
    const row = document.createElement("tr");
    cell(row, r.pattern);
    cell(row, r.reason);
    routes.appendChild(row);
  }
}

// ── resource instances and their supervision state ───────────────────────────

async function loadReadiness() {
  // 200 and 503 carry the same report. 503 is the answer while a required
  // instance is not serving, which is the answer most worth reading.
  const answer = await apiCall("GET", "/api/v1/readiness");
  const report = answer.body || {};

  el("readiness-verdict").textContent =
    (report.ready ? "ready" : "NOT ready") +
    ": " + report.serving + " of " + report.declared + " declared instances serving" +
    ((report.blocked || []).length ? ", blocked on " + report.blocked.join(", ") : "");
  el("readiness-verdict").className = "note " + (report.ready ? "ok" : "bad");

  const rows = body("resources");
  clear(rows);
  for (const instance of report.instances || []) {
    const row = document.createElement("tr");
    cell(row, instance.resource);
    cell(row, instance.state);
    cell(row, instance.required ? "yes" : "no");
    cell(row, instance.serving ? "yes" : "no", instance.serving ? "ok" : "bad");
    rows.appendChild(row);
  }

  return report;
}

// ── printers ─────────────────────────────────────────────────────────────────

// corrections counts the frames that have taught the roster something: a printer
// that went, one that became unreachable, and one whose link reading moved. It
// only ever goes up, so it orders a frame against a listing that was asked for
// before or after it.
//
// A listing describes the fleet as it was when the server produced it, and the
// page holds no version to order the two against. A row that arrives from a
// listing asked for before a frame the page has already applied would put back
// the fact the frame corrected, so that row is skipped and the frame's version
// stands. A listing asked for after the frame is the newer reading and wins.
//
// It is per printer rather than per listing: a busy fleet publishes a frame
// every few seconds, and dropping the whole answer would leave a page that never
// finishes loading its roster.
let corrections = 0;
const correctedAt = new Map();

function correct(id) {
  corrections += 1;
  correctedAt.set(id, corrections);
}

async function loadPrinters() {
  const asked = corrections;
  const listing = await readJSON("/api/v1/devices?limit=" + ROSTER_PAGE);

  for (const view of listing.items || []) {
    if ((correctedAt.get(view.id) || 0) > asked) { continue; }
    state.printers.set(view.id, { view: view });
  }

  renderRosterNote(listing);
  renderPrinters();
}

// renderRosterNote says when the fleet is larger than the page that was granted.
//
// The count is over every page and counts only what this caller may see, so a
// table shorter than it is a fleet this panel is not showing, and one that looks
// exactly like a smaller fleet. Which printers are missing is not knowable from
// here, so the note says how many and how to see the rest.
function renderRosterNote(listing) {
  const shown = (listing.items || []).length;
  const total = Number(listing.total) || 0;
  const offset = Number(listing.offset) || 0;
  const node = el("roster-note");

  node.hidden = total <= offset + shown;
  node.textContent = node.hidden ? "" :
    "Showing " + shown + " of " + total + " printers this identity may see. The server granted a page of "
    + (Number(listing.limit) || shown) + ", and this panel reads one page: the rest are reachable through "
    + "/api/v1/devices with an offset.";
}

// rate renders a per-minute figure at the precision it deserves. A storm is a
// round number and the first few frames of one are not.
function rate(value) {
  const figure = Number(value) || 0;
  return figure >= 10 ? String(Math.round(figure)) : figure.toFixed(1);
}

function span(link) {
  return (Number(link.measured_over_seconds) || 0).toFixed(1) + "s";
}

// linkOf is what this server has made of one printer's frames.
//
// The figures are the printer's session, not any one message: they say how much
// of what this machine sent was unreadable, and nothing about which frame. A
// driver that counts neither reports counted false, which renders as the absence
// of a measurement rather than as a clean link, because those are different
// facts and only one of them is good news.
function linkOf(view) {
  const link = view.link || {};
  if (!link.counted) { return { text: "not counted", className: "", alarming: false }; }

  if (link.refused_frames_per_minute > 0) {
    return {
      text: "refusing " + rate(link.refused_frames_per_minute) + "/min over " + span(link)
        + ", " + link.refused_frames + " this session",
      className: "bad",
      alarming: true,
    };
  }

  if (link.dropped_telemetry_per_minute > 0) {
    return {
      text: "dropping " + rate(link.dropped_telemetry_per_minute) + "/min over " + span(link)
        + ", " + link.dropped_telemetry + " this session",
      className: "warn",
      alarming: false,
    };
  }

  if (link.refused_frames || link.dropped_telemetry) {
    // A reading with no span behind it is the first one taken of this printer,
    // which carries the totals and no rate yet. Saying "none in the last 0.0s"
    // there would report a machine that has been refusing for an hour as one
    // that has stopped.
    const measured = Number(link.measured_over_seconds) || 0;
    return {
      text: link.refused_frames + " refused and " + link.dropped_telemetry
        + (measured > 0 ? " dropped earlier, none in the last " + span(link)
          : " dropped, no span measured yet"),
      className: "warn",
      alarming: false,
    };
  }

  return { text: "every frame read", className: "ok", alarming: false };
}

// renderAlarm puts an unreadable printer above every panel.
//
// It is the whole point of the column below it. The machine this is about looks
// idle everywhere else on the page, so a reader who has to go and find the
// column is reading the same silence they came here to explain.
function renderAlarm() {
  const alarming = [];

  // Which printers alarm is decided by linkOf and not a second time here. Two
  // copies of that rule is how a banner and the column under it come to
  // disagree about the same printer.
  for (const [id, entry] of state.printers) {
    if (gone(entry)) { continue; }
    if (linkOf(entry.view).alarming) {
      alarming.push(id + " at " + rate(entry.view.link.refused_frames_per_minute) + "/min");
    }
  }

  const node = el("link-alarm");
  node.hidden = alarming.length === 0;
  node.textContent = alarming.length === 0 ? "" :
    (alarming.length === 1 ? "1 printer is connected and sending frames this build cannot read: "
      : alarming.length + " printers are connected and sending frames this build cannot read: ")
    + alarming.join(", ");
}

function renderPrinters() {
  const rows = body("printers");
  clear(rows);

  for (const [id, entry] of state.printers) {
    const view = entry.view;
    const link = linkOf(view);
    const away = gone(entry);
    const row = document.createElement("tr");
    row.className = "selectable" + (away ? " gone" : "") + (id === state.selected ? " selected" : "");
    row.addEventListener("click", function () { select(id); });
    cell(row, id);
    cell(row, view.model);
    cell(row, view.serial);
    cell(row, view.firmware);
    cell(row, view.driver);
    cell(row, away ? "gone: " + goneReason(entry) : view.connected ? "open" : "away",
      away ? "bad" : view.connected ? "ok" : "warn");
    cell(row, away ? "" : link.text, away ? "" : link.className);
    cell(row, (view.capabilities || []).length);
    cell(row, view.capability_epoch);
    cell(row, view.profile_matched ? "matched" : "driver baseline", view.profile_matched ? "ok" : "warn");
    // What this caller may do to this printer rides on the listing already, so
    // a console that greyed a control would be reading it rather than trying
    // the action to find out.
    cell(row, (view.actions || []).join(" "));
    rows.appendChild(row);
  }

  renderAlarm();
}

// remember puts the selected printer in the address bar, so that what somebody
// found here is handed on as a link rather than as an instruction to go and
// click the right row.
function remember(id) {
  const here = new URL(window.location.href);
  here.searchParams.set("select", id);
  window.history.replaceState(null, "", here.toString());
}

// select opens one printer, and survives that printer not being there.
//
// A link handed to somebody else names a printer that is routinely absent when
// they open it: it has not dialled in yet, it departed, it is one they may not
// view, or the id was mistyped. All four answer the same refusal, and none of
// them is a reason to stop loading the page around it, because the live wire is
// what will say when the printer arrives.
async function select(id) {
  state.selected = id;
  remember(id);
  renderPrinters();

  const [known] = await Promise.all([loadDetail(id), loadCommands(id), loadDeviceStorages(id)]);

  el("detail-empty").hidden = true;
  el("detail").hidden = !known;
  el("detail-missing").hidden = known;
  el("detail-missing").textContent = known ? "" :
    "No printer with the id " + id + " has dialled in to this server, or this identity may not see it. "
    + "The selection stands, so this panel fills in if it arrives.";
}

// loadDetail renders one printer and reports whether there was one to render.
async function loadDetail(id) {
  const answer = await apiCall("GET", "/api/v1/devices/" + encodeURIComponent(id));
  let view = answer.body;

  if (!answer.ok) {
    // A departed printer answers the same refusal as one the caller may not
    // see. The listing entry is the last thing known about it, so it stands.
    const entry = state.printers.get(id);
    if (!entry) { return false; }
    view = entry.view;
  }

  const identity = body("detail-identity");
  clear(identity);
  const facts = [
    ["id", view.id],
    ["name", view.name],
    ["vendor", view.vendor],
    ["model", view.model],
    ["serial", view.serial],
    ["firmware", view.firmware],
    ["driver", view.driver],
    ["session", view.connected ? "open" : "away"],
    ["link", linkOf(view).text],
    ["capability epoch", view.capability_epoch],
    ["this caller may", (view.actions || []).join(", ")],
    ["authorization epoch", view.authorization_epoch],
    ["profile", view.profile_matched ? "matched" : "no profile covered this model, driver baseline in force"],
  ];
  for (const [label, value] of facts) {
    const row = document.createElement("tr");
    cell(row, label);
    cell(row, value);
    identity.appendChild(row);
  }

  renderIssues(view);

  const caps = el("capabilities");
  clear(caps);
  const capabilities = view.capabilities || [];
  el("capability-count").textContent = capabilities.length;
  for (const verb of capabilities) {
    const li = document.createElement("li");
    li.textContent = verb;
    caps.appendChild(li);
  }

  renderCommandForm(view);

  const telemetry = body("telemetry");
  clear(telemetry);
  const seen = state.seen.get(id) || new Map();
  const paths = Object.keys(view.state || {}).sort();
  for (const path of paths) {
    const row = document.createElement("tr");
    cell(row, path);
    cell(row, short(view.state[path], 120));
    cell(row, seen.has(path) ? time(seen.get(path)) : "");
    telemetry.appendChild(row);
  }

  return true;
}

// renderIssues shows what the host has found wrong with this printer. A machine
// that is connected and useless is the case the rest of this panel renders as an
// ordinary printer, so the reasons go beside the identity rather than in a
// column somebody has to widen.
function renderIssues(view) {
  const issues = view.issues || [];
  el("detail-issues-empty").hidden = issues.length > 0;

  const rows = body("detail-issues");
  clear(rows);

  for (const issue of issues) {
    const row = document.createElement("tr");
    cell(row, issue.reason);
    cell(row, issue.severity, issue.severity === "error" ? "bad" : "warn");
    cell(row, issue.since ? time(issue.since) : "");
    cell(row, issue.detail);
    rows.appendChild(row);
  }
}

async function loadCommands(id) {
  const answer = await apiCall("GET", "/api/v1/dev/devices/" + encodeURIComponent(id) + "/commands");
  const rows = body("commands");

  if (!answer.ok) {
    clear(rows);

    // The command log is reached through the printer's own lookup, so it answers
    // the same refusal for a printer that is not there and one this identity may
    // not act on. Saying so beats an empty table, which reads as a printer that
    // was never commanded.
    el("commands-dropped").textContent = "the command log did not answer: " + answer.detail;

    return;
  }

  const log = answer.body || {};

  el("commands-dropped").textContent = log.dropped
    ? log.dropped + " earlier commands have scrolled out of the log"
    : "";

  for (const entry of (log.items || []).slice().reverse()) {
    const row = document.createElement("tr");
    cell(row, time(entry.at));
    cell(row, entry.verb);
    cell(row, entry.args ? short(entry.args, 80) : "");
    cell(row, (entry.elapsed_ms || 0) + " ms");
    if (entry.failure) {
      cell(row, (entry.fault_code ? entry.fault_code + ": " : "") + entry.failure, "bad");
    } else {
      let outcome = entry.ok ? "accepted" : "refused";
      // The machine's own word for the refusal. It is the fact a person reading
      // this log came for, so it sits next to "refused" rather than in a column
      // that is empty for every other row.
      //
      // Whether a reason may accompany an accepted command is the device plane's
      // rule and is not repeated here. A row reading "accepted: FILE_EXISTS" is
      // that rule broken upstream, and this page exists to show it.
      if (entry.machine_reason) { outcome += ": " + entry.machine_reason; }
      if (entry.ignored) { outcome += ", ignored " + entry.ignored.join(", "); }
      if (entry.substituted) { outcome += ", substituted " + short(entry.substituted, 60); }
      cell(row, outcome, entry.ok ? "ok" : "bad");
    }
    rows.appendChild(row);
  }
}

// ── sending a command ────────────────────────────────────────────────────────

// loadCapabilities reads the capability vocabulary once. It is what tells the
// command form which arguments a verb takes, so the form is generated from the
// same set the server validates against rather than from a list typed here.
async function loadCapabilities() {
  const vocabulary = await readJSON("/api/v1/vocabularies/capabilities");

  state.capabilities = new Map();

  for (const key of vocabulary.keys || []) {
    if (!key.verb) {
      if (!state.capabilities.has(key.key)) { state.capabilities.set(key.key, []); }
      continue;
    }

    if (!state.capabilities.has(key.verb)) { state.capabilities.set(key.verb, []); }
    state.capabilities.get(key.verb).push(key);
  }
}

// renderCommandForm offers the verbs this printer carries and nothing else.
//
// A capability set is per printer and per firmware, so a form listing every verb
// this build knows would offer commands the machine in front of the reader will
// refuse. The arguments come from the vocabulary, keyed by the verb, which is
// the same set the server checks the call against.
function renderCommandForm(view) {
  const verbs = (view.capabilities || []).filter(function (key) { return key.indexOf("#") < 0; });

  el("command-empty").hidden = true;
  el("command-form").hidden = false;

  fill("command-verb", verbs, verbs.length > 0 ? "pick a verb" : "this printer carries no verb");
  renderCommandArgs();
}

// renderCommandArgs draws one field per argument the selected verb declares.
// Required arguments are marked, because the refusal for a missing one arrives
// from the server and reads as a rejected command rather than an empty field.
function renderCommandArgs() {
  const verb = el("command-verb").value;
  const fields = el("command-args");
  clear(fields);
  state.commandFields = new Map();

  for (const argument of state.capabilities.get(verb) || []) {
    const label = document.createElement("label");
    label.textContent = argument.argument + " (" + argument.kind
      + (argument.required ? ", required" : "") + ")";

    const input = document.createElement("input");
    input.type = "text";
    input.spellcheck = false;

    label.appendChild(input);
    fields.appendChild(label);
    state.commandFields.set(argument.argument, input);
  }
}

// collectArgs reads the generated fields back. An empty optional field is left
// out rather than sent empty: the two mean different things to a printer, and
// only one of them is what a reader who skipped a field meant.
function collectArgs() {
  const args = {};

  for (const [name, input] of state.commandFields) {
    if (input.value !== "") { args[name] = input.value; }
  }

  return args;
}

async function sendCommand() {
  const verb = el("command-verb").value;

  if (!state.selected || !verb) {
    say("command-outcome", "pick a printer and a verb first", "warn");
    return;
  }

  const answer = await apiCall("POST",
    "/api/v1/devices/" + encodeURIComponent(state.selected) + "/commands",
    { body: { verb: verb, args: collectArgs() } });

  if (!answer.ok) {
    say("command-outcome", answer.status + " " + answer.detail, "bad");
    return;
  }

  const result = answer.body || {};
  let outcome = result.ok ? "the printer accepted it" : "the printer refused it";
  if (result.machine_reason) { outcome += ": " + result.machine_reason; }
  if (result.ignored && result.ignored.length) { outcome += ", ignored " + result.ignored.join(", "); }
  if (result.substituted) { outcome += ", substituted " + JSON.stringify(result.substituted); }

  say("command-outcome", outcome, result.ok ? "ok" : "bad");
  loadCommands(state.selected).catch(function () {});
}

async function sendGCode() {
  if (!state.selected) {
    say("gcode-outcome", "pick a printer first", "warn");
    return;
  }

  const commands = lines(el("gcode-body").value);
  if (commands.length === 0) {
    say("gcode-outcome", "there is nothing to run", "warn");
    return;
  }

  const answer = await apiCall("POST",
    "/api/v1/devices/" + encodeURIComponent(state.selected) + "/gcode",
    { body: { lines: commands, force: el("gcode-force").checked } });

  say("gcode-outcome", answer.ok ? gcodeOutcome(answer.body) : answer.status + " " + answer.detail,
    answer.ok && !answer.body.stopped ? "ok" : "bad");
  loadCommands(state.selected).catch(function () {});
}

// gcodeOutcome says how far a run got and what stopped it. A run reports one
// entry per line, and the line that stopped it is the only one a reader needs
// before the rest.
function gcodeOutcome(outcome) {
  const steps = (outcome && outcome.steps) || [];
  const refused = steps.filter(function (step) { return step.status === "refused"; });

  let text = (outcome.completed || 0) + " of " + steps.length + " accepted";
  if (outcome.stopped) { text += ", the run stopped"; }

  for (const step of refused) {
    text += " | " + step.gcode + ": " + (step.machine_reason || step.detail || step.code);
  }

  return text;
}

async function runPreset() {
  const preset = el("gcode-preset").value;

  if (!state.selected || !preset) {
    say("preset-run-outcome", "pick a printer and a sequence first", "warn");
    return;
  }

  const answer = await apiCall("POST",
    "/api/v1/devices/" + encodeURIComponent(state.selected)
    + "/gcode/presets/" + encodeURIComponent(preset), { body: {} });

  say("preset-run-outcome", answer.ok ? gcodeOutcome(answer.body) : answer.status + " " + answer.detail,
    answer.ok && !answer.body.stopped ? "ok" : "bad");
  loadCommands(state.selected).catch(function () {});
}

// ── the printer's own storage ────────────────────────────────────────────────

async function loadDeviceStorages(id) {
  const answer = await apiCall("GET", "/api/v1/devices/" + encodeURIComponent(id) + "/storages");
  if (!answer.ok) { return; }

  const rows = body("device-storages");
  clear(rows);

  for (const storage of (answer.body && answer.body.items) || []) {
    const row = document.createElement("tr");
    row.className = "selectable";
    row.addEventListener("click", function () {
      el("device-path").value = storage.name || storage.mountpoint;
      listDeviceFiles();
    });
    cell(row, storage.mountpoint);
    cell(row, storage.name);
    cell(row, storage.type);
    cell(row, storage.read_only ? "read only" : "writable");
    cell(row, storage.free_space === null ? "not reported" : storage.free_space);
    rows.appendChild(row);
  }
}

async function listDeviceFiles() {
  const rows = body("device-files");

  if (!state.selected) {
    say("device-files-outcome", "pick a printer first", "warn");
    return;
  }

  const path = el("device-path").value.replace(/^\//, "");
  const answer = await apiCall("GET",
    "/api/v1/devices/" + encodeURIComponent(state.selected)
    + "/files?path=" + encodeURIComponent(path));

  clear(rows);

  if (!answer.ok) {
    say("device-files-outcome", answer.status + " " + answer.detail, "bad");
    return;
  }

  const listing = answer.body || {};
  const items = listing.items || [];
  const outcome = items.length + " entries under " + (path || "the root");
  if (listing.fault) {
    say("device-files-outcome",
      outcome + ": " + listing.fault.code + " " + (listing.fault.detail || listing.fault.title), "bad");
  } else if (listing.reading) {
    say("device-files-outcome", outcome + " (reading since " + listing.reading_since + ")", "warn");
  } else {
    say("device-files-outcome", outcome, "ok");
  }

  for (const entry of items) {
    const row = document.createElement("tr");
    if (entry.folder) {
      row.className = "selectable";
      row.addEventListener("click", function () {
        el("device-path").value = entry.path;
        listDeviceFiles();
      });
    }
    cell(row, entry.display_name || entry.name);
    cell(row, entry.folder ? "folder" : "file");
    cell(row, entry.folder ? "" : entry.size);
    cell(row, entry.modified ? new Date(entry.modified * 1000).toISOString().slice(0, 19) : "");
    rows.appendChild(row);
  }
}

// ── the live wire ────────────────────────────────────────────────────────────

function setWireState(text, className) {
  const node = el("wire-state");
  node.textContent = "wire: " + text;
  node.className = "pill " + (className || "");
}

function parseFrame(block) {
  const frame = { id: "", event: "", data: "" };
  let carried = false;

  for (const line of block.split("\n")) {
    if (line === "" || line.startsWith(":")) { continue; }
    const colon = line.indexOf(":");
    const field = colon < 0 ? line : line.slice(0, colon);
    let value = colon < 0 ? "" : line.slice(colon + 1);
    if (value.startsWith(" ")) { value = value.slice(1); }

    if (field === "id") { frame.id = value; carried = true; }
    else if (field === "event") { frame.event = value; carried = true; }
    else if (field === "data") { frame.data += value; carried = true; }
  }

  return carried ? frame : null;
}

function onFrame(frame) {
  // A block carrying an id and nothing else is the cursor this server stamps on
  // a window that moved a reader's place without publishing an event. It is
  // already read for the resume position, and filing it as an event would put a
  // row with no type and no payload in the store and a blank line at the top of
  // the type counts.
  if (frame.event === "" && frame.data === "") { return; }

  let envelope = null;
  try { envelope = JSON.parse(frame.data); } catch (err) { envelope = null; }

  const record = {
    at: envelope && envelope.time ? new Date(envelope.time) : new Date(),
    type: frame.event || (envelope && envelope.type) || "",
    subject: (envelope && envelope.subject) || "",
    source: (envelope && envelope.source) || "",
    data: envelope && envelope.data !== undefined ? envelope.data : frame.data,
    envelope: envelope,
  };

  storeEvent(record);
  applyFrame(record, envelope);
}

// scheduleDetail bounds how often the selected printer is re-read. Telemetry
// arrives every coalescing window, and one request per window is a poll nobody
// asked for.
let detailPending = 0;

function scheduleDetail(id) {
  const now = Date.now();
  if (now - detailPending < DETAIL_INTERVAL) { return; }
  detailPending = now;
  loadDetail(id).catch(function () {});
}

// applyFrame is what the stream teaches the rest of the page: a device that
// arrived, one that departed, and when each telemetry path was last reported.
function applyFrame(record, envelope) {
  const id = record.subject.startsWith("device:") ? record.subject.slice("device:".length) : "";
  if (!id) { return; }

  if (record.type === TELEMETRY_TYPE && envelope && envelope.data && envelope.data.path) {
    if (!state.seen.has(id)) { state.seen.set(id, new Map()); }
    state.seen.get(id).set(envelope.data.path, record.at);
    if (id === state.selected) { scheduleDetail(id); }
    return;
  }

  if (record.type === PRESENCE_TYPE) {
    const entry = state.printers.get(id);
    if (entry && envelope && envelope.data) {
      entry.view.connected = envelope.data.connected === true;
      correct(id);
      renderPrinters();
    }
    return;
  }

  if (record.type === LINK_TYPE) {
    const entry = state.printers.get(id);
    if (entry && envelope && envelope.data && envelope.data.link) {
      entry.view.link = envelope.data.link;
      correct(id);
      renderPrinters();
    }
    return;
  }

  if (record.type === DEPARTED_TYPE) {
    const entry = state.printers.get(id);
    if (entry) {
      entry.view.connected = false;
      entry.view.departed = { reason: (envelope && envelope.data && envelope.data.reason) || "" };
      correct(id);
      renderPrinters();
    }
    return;
  }

  if (record.type === ARRIVED_TYPE) {
    scheduleRoster();
  }
}

// scheduleRoster coalesces the re-read an arrival asks for, the way
// scheduleDetail coalesces the one telemetry asks for.
let rosterPending = null;

function scheduleRoster() {
  if (rosterPending !== null) { return; }
  rosterPending = setTimeout(function () {
    rosterPending = null;
    loadPrinters().catch(function () {});
  }, ROSTER_COALESCE);
}

// ── the printers area, loaded ────────────────────────────────────────────────

async function refresh() {
  const jobs = [
    ["connectors", loadConnectors()],
    ["readiness", loadReadiness()],
    ["printers", loadPrinters()],
    ["capabilities", loadCapabilities()],
  ];

  for (const [what, job] of jobs) {
    try {
      await job;
    } catch (err) {
      console.error("loading " + what, err);
    }
  }

  if (state.selected) {
    await select(state.selected);
  }
}

function wireFleet() {
  el("command-verb").addEventListener("change", renderCommandArgs);
  el("command-send").addEventListener("click", sendCommand);
  el("gcode-send").addEventListener("click", sendGCode);
  el("preset-run").addEventListener("click", runPreset);
  el("device-list").addEventListener("click", listDeviceFiles);
  el("calls-clear").addEventListener("click", function () { state.calls.length = 0; renderCalls(); });
}
