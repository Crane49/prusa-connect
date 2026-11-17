// The events area: the stream, the store it fills, and the search over it.
//
// The store is this page's own. The server keeps a bounded replay window per
// origin and hands it back only to a reader that resumes from a place in it, so
// the only way to hold a session's events and look through them afterwards is to
// keep them here. That is what EVENT_STORE bounds.

// EVENT_STORE is how many events the console holds. It is far above the wire
// panel's old bound because this store is searched rather than scrolled, and a
// filter over four hundred events answers questions a filter over four hundred
// thousand would answer better and a browser would not survive.
const EVENT_STORE = 20000;

// EVENT_ROWS is how many matching events are drawn. The summary reports the
// whole match, so a filter that narrows to more than this says so rather than
// showing a table that quietly stops.
const EVENT_ROWS = 400;

// CONTINUITY_TYPE is the frame the transport authors when this reader's place in
// the log is gone. It is not an event the hub published, and it is the one frame
// that means the store below it has a hole in it.
const CONTINUITY_TYPE = "com.prusa.connect.stream.continuity.lost";

// EXPIRED_TYPE is the other frame the transport authors: the credential that
// opened this stream stopped being one, so the reader was logged out rather than
// disconnected.
const EXPIRED_TYPE = "prusa-connect.auth.expired";

const events = {
  store: [],
  // dropped counts what the store evicted, so a search over a full store is
  // never read as a search over the whole session.
  dropped: 0,
  types: new Map(),      // type -> {count, first, last}
  origins: new Set(),
  // declared is every type this build publishes, read from the vocabulary, so a
  // reader can pick one that has never arrived and see that it has not.
  declared: [],
  paused: false,
  lastID: "",
  selected: null,
  // epoch identifies the stream this page is reading. A reopen moves it, and
  // the loop reading the old one stops when it sees that it has.
  epoch: 0,
  abort: null,
};

// storeEvent is what every frame off the wire lands in. It is called from the
// reader in page.js, so the store and the panels that teach themselves from the
// stream see exactly the same records in the same order.
function storeEvent(record) {
  if (events.paused) { return; }

  events.store.unshift(record);
  if (events.store.length > EVENT_STORE) {
    events.store.length = EVENT_STORE;
    events.dropped += 1;
  }

  const seen = events.types.get(record.type);
  if (seen) {
    seen.count += 1;
    seen.last = record.at;
  } else {
    events.types.set(record.type, { count: 1, first: record.at, last: record.at });
  }

  if (record.subject) { events.origins.add(record.subject); }

  if (record.type === CONTINUITY_TYPE) { announceContinuity(record); }
  if (record.type === EXPIRED_TYPE) {
    setWireState("the credential expired, so the stream ended", "bad");
  }

  scheduleEvents();
}

// announceContinuity says that the store has a hole in it.
//
// It is a banner and not a row, because a reader searching a store that is
// missing a stretch of the session gets the same empty answer as a reader whose
// filter matched nothing, and those are different facts.
function announceContinuity(record) {
  const data = record.data || {};
  const node = el("events-continuity");

  node.hidden = false;
  node.textContent = "The server said this reader's place in the log was gone at " + time(record.at)
    + ", after " + (data.dropped || 0) + " events. Everything before the cursor " + (data.cursor || "")
    + " is missing from this store, and the printers list was re-read from the API.";

  loadPrinters().catch(function () {});
}

// scheduleEvents coalesces the redraw. A window off the hub can carry a whole
// fleet's metric set, and repainting a four hundred row table once per frame is
// what turns a busy fleet into a page that cannot be scrolled.
let eventsPending = false;

function scheduleEvents() {
  if (eventsPending) { return; }
  eventsPending = true;
  requestAnimationFrame(function () { eventsPending = false; renderEvents(); });
}

// ── the search ───────────────────────────────────────────────────────────────

// matches is the whole filter. Every field is applied together, and an empty
// field is not a filter, so the default is the whole store.
function matches(record, filter) {
  if (filter.type && record.type.indexOf(filter.type) < 0) { return false; }
  if (filter.origin && record.subject.indexOf(filter.origin) < 0) { return false; }

  if (filter.from || filter.to) {
    const at = time(record.at).slice(0, 8);
    if (filter.from && at < filter.from) { return false; }
    if (filter.to && at > filter.to) { return false; }
  }

  if (filter.text) {
    const payload = JSON.stringify(record.envelope === null ? record.data : record.envelope);
    if (!payload || payload.toLowerCase().indexOf(filter.text) < 0) { return false; }
  }

  return true;
}

function readFilter() {
  return {
    type: el("events-type").value,
    origin: el("events-origin").value,
    text: el("events-text").value.trim().toLowerCase(),
    from: el("events-from").value.trim(),
    to: el("events-to").value.trim(),
  };
}

function renderEvents() {
  const filter = readFilter();
  const rows = body("events-rows");
  clear(rows);

  let matched = 0;

  for (const record of events.store) {
    if (!matches(record, filter)) { continue; }
    matched += 1;
    if (matched > EVENT_ROWS) { continue; }

    const row = document.createElement("tr");
    row.className = "selectable" + (record === events.selected ? " selected" : "");
    row.addEventListener("click", function () { showEnvelope(record); });
    cell(row, time(record.at));
    cell(row, record.type);
    cell(row, record.subject);
    cell(row, short(record.data, 300));
    rows.appendChild(row);
  }

  renderEventSummary(matched);
  renderEventCounts();
  refreshEventChoices();
}

function renderEventSummary(matched) {
  const held = events.store.length;
  const shown = Math.min(matched, EVENT_ROWS);

  let text = held + " events held of a bound of " + EVENT_STORE + ", " + matched + " match, "
    + shown + " drawn";
  if (events.dropped > 0) {
    text += ". " + events.dropped + " older events have been evicted from this store, so a search over "
      + "them answers empty and the session was longer than this";
  }
  if (events.paused) { text += ". The store is paused, so frames arriving now are being discarded"; }

  say("events-summary", text, events.dropped > 0 || events.paused ? "warn" : "");
}

function renderEventCounts() {
  const rows = body("events-counts");
  clear(rows);

  const seen = Array.from(events.types.entries())
    .sort(function (left, right) { return right[1].count - left[1].count; });

  for (const [type, tally] of seen) {
    const row = document.createElement("tr");
    row.className = "selectable";
    row.addEventListener("click", function () {
      el("events-type").value = type;
      renderEvents();
    });
    cell(row, type);
    cell(row, tally.count);
    cell(row, time(tally.first));
    cell(row, time(tally.last));
    rows.appendChild(row);
  }
}

// refreshEventChoices keeps the two pickers over what has actually arrived. A
// type that has never arrived is still on the list, because the vocabulary is
// read at startup: a reader looking for an event that never came needs to be
// able to select it and see nothing, which is the answer they came for.
//
// The two lists are rebuilt only when their contents change. A busy fleet
// redraws this table several times a second, and replacing a select's options
// under a reader closes the list they had just opened.
let choicesShown = "";

function refreshEventChoices() {
  const types = Array.from(events.types.keys());
  for (const declared of events.declared || []) {
    if (!events.types.has(declared)) { types.push(declared); }
  }

  types.sort();
  const origins = Array.from(events.origins).sort();

  // A type and an origin cannot carry a space, so joining on one identifies
  // the pair of lists without a separator that could appear inside a member.
  const signature = types.join(" ") + " || " + origins.join(" ");
  if (signature === choicesShown) { return; }
  choicesShown = signature;

  fill("events-type", types, "every type");
  fill("events-origin", origins, "every origin");
}

function showEnvelope(record) {
  events.selected = record;
  el("events-detail").textContent =
    JSON.stringify(record.envelope === null ? record : record.envelope, null, 2);
  renderEvents();
}

// loadEventTypes reads the vocabulary, so the type picker offers a type this
// build publishes before one has been seen.
async function loadEventTypes() {
  const vocabulary = await readJSON("/api/v1/vocabularies/event-types");

  events.declared = (vocabulary.items || []).map(function (entry) { return entry.type; });
  refreshEventChoices();
}

// ── the stream ───────────────────────────────────────────────────────────────

// streamPath is the request the two controls make. Origins narrows the stream on
// the server rather than in the store, so an origin this caller may not observe
// refuses the whole request: that refusal is the point of the control, and it is
// a thing the filter above cannot ever tell a reader.
function streamPath() {
  const query = [];
  const origins = commas(el("events-origins").value);
  const interval = el("events-interval").value.trim();

  if (origins.length > 0) { query.push("origins=" + encodeURIComponent(origins.join(","))); }
  if (interval !== "") { query.push("interval_ms=" + encodeURIComponent(interval)); }

  return "/api/v1/events" + (query.length > 0 ? "?" + query.join("&") : "");
}

// openStream reopens the wire under the controls as they now read. The epoch is
// what stops the loop that was reading the old one: a reader that kept running
// would go on filling the store from a stream the reader has replaced.
function openStream() {
  events.epoch += 1;

  if (events.abort) {
    try { events.abort.abort(); } catch (err) { /* an aborted fetch is the point */ }
  }

  events.abort = typeof AbortController === "function" ? new AbortController() : null;

  pumpWire(events.epoch, events.abort);
}

async function pumpWire(epoch, controller) {
  while (epoch === events.epoch) {
    const path = streamPath();

    try {
      setWireState("connecting");

      const headers = { Accept: "text/event-stream" };
      // Last-Event-ID is how the server's own replay window is read. It is the
      // only history this API offers: the hub keeps a bounded log per origin
      // and hands back the part of it after the place a reader names.
      if (events.lastID) { headers["Last-Event-ID"] = events.lastID; }

      const init = { headers: headers };
      if (controller) { init.signal = controller.signal; }

      const started = Date.now();
      const resp = await fetch(path, init);

      recordCall({
        at: new Date(), method: "GET", path: path, status: resp.status, ok: resp.ok,
        body: null, detail: resp.ok ? (events.lastID ? "resumed from " + events.lastID : "opened") : "",
        version: "", elapsed: Date.now() - started,
      });

      if (!resp.ok || !resp.body) {
        throw new Error("the event stream answered " + resp.status);
      }

      setWireState("open", "ok");

      // The roster is reconciled here and nowhere on a clock. Arrivals,
      // departures, reachability and the link reading all reach the page as
      // frames, so the only thing the page cannot learn from the stream is what
      // happened while it was not reading one.
      loadPrinters().catch(function () {});

      await drain(resp.body.getReader(), epoch);

      setWireState("the stream ended, reconnecting", "warn");
    } catch (err) {
      if (epoch !== events.epoch) { return; }
      setWireState("reconnecting after " + err.message, "bad");
    }

    if (epoch !== events.epoch) { return; }

    await new Promise(function (done) { setTimeout(done, RECONNECT_DELAY); });
  }
}

// drain reads one connection to its end, or until the stream it belongs to has
// been replaced.
async function drain(reader, epoch) {
  const decoder = new TextDecoder();
  let buffer = "";

  for (;;) {
    const chunk = await reader.read();
    if (chunk.done || epoch !== events.epoch) { return; }

    buffer += decoder.decode(chunk.value, { stream: true }).replace(/\r\n/g, "\n");

    let cut = buffer.indexOf("\n\n");
    while (cut >= 0) {
      const frame = parseFrame(buffer.slice(0, cut));
      buffer = buffer.slice(cut + 2);
      if (frame) {
        if (frame.id) { events.lastID = frame.id; }
        onFrame(frame);
      }
      cut = buffer.indexOf("\n\n");
    }
  }
}

function wireEvents() {
  el("events-reconnect").addEventListener("click", function () {
    say("events-summary", "reopening the stream" + (events.lastID
      ? ", resuming from " + events.lastID + ", so the server replays what it still holds"
      : ""), "");
    openStream();
  });

  el("events-pause").addEventListener("click", function () {
    events.paused = !events.paused;
    el("events-pause").textContent = events.paused ? "resume" : "pause";
    renderEvents();
  });

  el("events-clear").addEventListener("click", function () {
    events.store.length = 0;
    events.dropped = 0;
    events.types = new Map();
    events.selected = null;
    el("events-detail").textContent = "Pick a row.";
    el("events-continuity").hidden = true;
    renderEvents();
  });

  el("events-apply").addEventListener("click", renderEvents);
  el("events-type").addEventListener("change", renderEvents);
  el("events-origin").addEventListener("change", renderEvents);

  el("events-reset").addEventListener("click", function () {
    el("events-type").value = "";
    el("events-origin").value = "";
    el("events-text").value = "";
    el("events-from").value = "";
    el("events-to").value = "";
    renderEvents();
  });
}
