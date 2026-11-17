// The shell: which area is on screen, and the order the console starts in.
//
// It is the last file in the script because it is the only one that runs
// anything. Every file before it declares and wires, so a panel that is broken
// is broken where it is written rather than in whatever ran first.

// areas are the five things this console covers, in the order the tab strip
// shows them. Each names its tab, its view and what reloads it, so the strip,
// the address bar and the refresh button all read one list.
const areas = [
  { name: "printers", tab: "tab-fleet", view: "view-fleet", load: function () { return refresh(); } },
  { name: "events", tab: "tab-events", view: "view-events", load: function () { return loadEventTypes(); } },
  { name: "access", tab: "tab-access", view: "view-access", load: function () { return loadAccess(); } },
  { name: "config", tab: "tab-config", view: "view-config", load: function () { return loadConfiguration(); } },
  { name: "setup", tab: "tab-setup", view: "view-setup", load: function () { return loadSetup(); } },
];

let showing = areas[0];

// show puts one area on screen and reloads it.
//
// It reloads on every visit rather than once. A console whose configuration tab
// showed what the server said the first time it was opened is one that lies to
// the person who just changed something from a terminal, and this page exists
// to be the thing they check against.
function show(name) {
  const wanted = areas.filter(function (area) { return area.name === name; });
  showing = wanted.length > 0 ? wanted[0] : areas[0];

  for (const area of areas) {
    el(area.view).hidden = area !== showing;
    el(area.tab).className = area === showing ? "selected" : "";
  }

  const here = new URL(window.location.href);
  here.searchParams.set("tab", showing.name);
  window.history.replaceState(null, "", here.toString());

  showing.load().catch(function (err) { console.error("loading " + showing.name, err); });
}

// wanted reads one query parameter. The address bar carries which area is open
// and which printer is selected, so what somebody found is handed on as a link.
function wanted(key) {
  return new URL(window.location.href).searchParams.get(key) || "";
}

async function start() {
  const health = await apiCall("GET", "/api/v1/health");
  el("build").textContent = health.ok && health.body
    ? "build " + health.body.version
    : "the health endpoint answered " + health.status;

  wireFleet();
  wireEvents();
  wireAccess();
  wireConfig();
  wireSetup();

  el("refresh").addEventListener("click", function () { show(showing.name); });
  for (const area of areas) {
    el(area.tab).addEventListener("click", function () { show(area.name); });
  }

  // A link somebody was handed opens on the printer it is about. The printer may
  // not have dialled in yet, in which case the detail panel answers empty and
  // the selection stands for when it does.
  const selected = wanted("select");
  if (selected) { state.selected = selected; }

  // The stream is subscribed before the panels are filled, and not after.
  //
  // Every panel below is a request some deployment refuses or never answers, and
  // a page that opened the wire last would wait on all of them first. Catching
  // does not reach the second case: an awaited request that never settles holds
  // up everything after it for ever, and what it holds up is the one panel that
  // would have shown the recovery. The reader who opens this page because a
  // printer has gone quiet is exactly the reader whose first load is unwell.
  openStream();

  // Four reads happen whatever area is open: the identity every panel is served
  // under, the login this deployment offers, the saved sequences the printers
  // area runs, and the event types the search offers before one has arrived.
  //
  // None is awaited before the area opens. The roster the area reads has to be
  // asked for before the stream's first frames rather than after them: a listing
  // asked for later is the newer reading and rightly wins, so a console that
  // waited on four unrelated requests first would put back the state that the
  // frames arriving in the meantime had already corrected.
  const shared = [
    ["the principal", loadPrincipal()],
    ["the login this deployment serves", loadAuthInfo()],
    ["the gcode presets", loadPresets()],
    ["the event types", loadEventTypes()],
  ];

  for (const [what, job] of shared) {
    job.catch(function (err) { console.error("loading " + what, err); });
  }

  show(wanted("tab") || areas[0].name);
}

start();
