// The configuration area: the resource instances the supervisor runs, the
// addresses this server dials, the saved gcode sequences, and the sets this
// build publishes about itself.
//
// Every write here carries the fencing token the read returned. The API refuses
// a write with no precondition, so a console that dropped it would be a console
// that could never save anything, and one that invented `*` would be a console
// that quietly overwrote whoever wrote last.

const configuration = {
  resources: [],
  // selected is the instance the editor is pointed at: its kind, its name and
  // the version a save quotes back. An empty version is an instance that does
  // not exist yet, which is written with If-None-Match instead.
  selected: null,
  endpoints: [],
  presets: [],
};

// reloadOverlay re-reads all three listings, and it is what follows every write.
//
// The version a write quotes is not the row's: it is the whole configuration
// overlay's, and it moves whenever any instance, endpoint or sequence is
// written. So a console that re-read only the list it had just written to would
// hold a stale token for the other two, and the next write to either would be
// refused with 412 quoting a version the reader never saw.
async function reloadOverlay() {
  await loadResources();
  await loadEndpoints();
  await loadPresets();
}

// report writes a panel's outcome after the reload, and never before it. A
// reload reports what it read, so an outcome written first is replaced by
// "1 endpoints" a moment later and every write looks like it did nothing.
function report(panel, answer, done) {
  say(panel, answer.ok ? done : answer.status + " " + answer.detail, answer.ok ? "ok" : "bad");
}

// ── resource instances ───────────────────────────────────────────────────────

// specOf is the writable half of an instance. The read carries status, source,
// uid and generation, all of which the write ignores, so echoing the whole
// record back would put a reader in front of eight fields that do nothing.
function specOf(instance) {
  return {
    type: instance.type,
    enabled: instance.enabled !== false,
    required: instance.required === true,
    config: instance.config || {},
  };
}

async function loadResources() {
  const answer = await apiCall("GET", "/api/v1/resources");
  const rows = body("config-resources");
  clear(rows);

  if (!answer.ok) {
    say("resource-outcome", answer.status + " " + answer.detail, "bad");
    return;
  }

  configuration.resources = (answer.body && answer.body.items) || [];

  for (const instance of configuration.resources) {
    const status = instance.status || {};
    const row = document.createElement("tr");
    const chosen = configuration.selected
      && configuration.selected.kind === instance.kind
      && configuration.selected.name === instance.name;

    row.className = "selectable" + (chosen ? " selected" : "");
    row.addEventListener("click", function () { selectResource(instance); });
    cell(row, instance.kind);
    cell(row, instance.name);
    cell(row, instance.type);
    cell(row, status.state, status.state === "ready" ? "ok" : status.state === "failed" ? "bad" : "warn");
    cell(row, status.serving ? "yes" : "no", status.serving ? "ok" : "bad");
    cell(row, instance.source, instance.source === "file" ? "warn" : "");
    cell(row, instance.version);
    rows.appendChild(row);
  }
}

function selectResource(instance) {
  configuration.selected = {
    kind: instance.kind, name: instance.name, version: instance.version, source: instance.source,
  };

  el("resource-spec").value = JSON.stringify(specOf(instance), null, 2);

  const status = instance.status || {};
  const why = (status.conditions || []).map(function (condition) {
    return condition.type + "=" + condition.status + " (" + condition.reason + ")"
      + (condition.message ? " " + condition.message : "");
  }).join(" | ");

  say("resource-selected", instance.kind + "/" + instance.name + " at version " + instance.version
    + ", declared in " + (instance.source === "file" ? "the operator's own configuration file, which this "
      + "console cannot write" : "the overlay the API owns")
    + (why ? ". " + why : ""), instance.source === "file" ? "warn" : "");

  say("resource-outcome", "", "");
  loadResources();
}

function resourcePath(selected) {
  return "/api/v1/resources/" + encodeURIComponent(selected.kind) + "/" + encodeURIComponent(selected.name);
}

// preconditionFor is what a write carries. A version that was read is quoted
// back, so a write refused with 412 is somebody else having written in between;
// an instance that is not there yet asks to be created and nothing else.
function preconditionFor(selected) {
  return selected.version ? { ifMatch: '"' + selected.version + '"' } : { ifNoneMatch: "*" };
}

async function saveResource() {
  const selected = configuration.selected;
  if (!selected) {
    say("resource-outcome", "pick an instance on the list, or start a new one below", "warn");
    return;
  }

  let spec;
  try {
    spec = JSON.parse(el("resource-spec").value);
  } catch (err) {
    say("resource-outcome", "that is not JSON: " + err.message, "bad");
    return;
  }

  const options = preconditionFor(selected);
  options.body = spec;

  const answer = await apiCall("PUT", resourcePath(selected), options);

  if (answer.ok) {
    configuration.selected = null;
    say("resource-selected", "Pick an instance.", "");
  }

  await reloadOverlay();
  await loadReadiness();

  report("resource-outcome", answer,
    answer.status + ", written. The supervisor reconciles the instance to it.");
}

async function restartResource() {
  const selected = configuration.selected;
  if (!selected) {
    say("resource-outcome", "pick an instance on the list first", "warn");
    return;
  }

  const answer = await apiCall("POST", resourcePath(selected) + "/restart", { body: {} });

  await loadResources();
  await loadReadiness();

  report("resource-outcome", answer, "restarting " + selected.kind + "/" + selected.name);
}

async function deleteResource() {
  const selected = configuration.selected;
  if (!selected || !selected.version) {
    say("resource-outcome", "pick an instance that exists first", "warn");
    return;
  }

  const gone = selected.kind + "/" + selected.name;
  const answer = await apiCall("DELETE", resourcePath(selected),
    { ifMatch: '"' + selected.version + '"' });

  if (answer.ok) {
    configuration.selected = null;
    el("resource-spec").value = "";
    say("resource-selected", "Pick an instance.", "");
  }

  await reloadOverlay();
  await loadReadiness();

  report("resource-outcome", answer, "deleted " + gone);
}

// startNewResource points the editor at an instance that does not exist yet. The
// template is the shape the API requires and nothing more: what belongs under
// config is the implementation's own, so it starts empty and the 422 that comes
// back names the key it wanted.
function startNewResource() {
  const kind = el("resource-new-kind").value;
  const name = el("resource-new-name").value.trim();

  if (!kind || !name) {
    say("resource-outcome", "a kind and a name are both needed", "warn");
    return;
  }

  configuration.selected = { kind: kind, name: name, version: "", source: "overlay" };
  el("resource-spec").value = JSON.stringify(
    { type: "", enabled: true, required: false, config: {} }, null, 2);

  say("resource-selected", "a new " + kind + " called " + name
    + ", which does not exist yet. Saving it asks the server to create it and nothing else, so a name "
    + "already taken is refused rather than overwritten.", "warn");
  say("resource-outcome", "", "");
  loadResources();
}

async function loadResourceKinds() {
  const vocabulary = await readJSON("/api/v1/vocabularies/resource-kinds");

  fill("resource-new-kind", (vocabulary.items || [])
    .filter(function (entry) { return entry.configurable; })
    .map(function (entry) { return entry.kind; }), "pick a kind");
}

// ── the addresses this server dials ──────────────────────────────────────────

async function loadEndpoints() {
  const answer = await apiCall("GET", "/api/v1/connector-endpoints");
  const rows = body("endpoints");
  clear(rows);

  if (!answer.ok) {
    say("endpoint-outcome", answer.status + " " + answer.detail
      + ". Reading these takes view_resource on the driver kind.", "bad");
    return;
  }

  configuration.endpoints = (answer.body && answer.body.items) || [];

  for (const endpoint of configuration.endpoints) {
    const row = document.createElement("tr");
    row.className = "selectable";
    row.addEventListener("click", function () { selectEndpoint(endpoint); });
    cell(row, endpoint.name);
    cell(row, endpoint.connector);
    cell(row, endpoint.address);
    cell(row, endpoint.enabled ? "yes" : "no", endpoint.enabled ? "ok" : "warn");
    cell(row, JSON.stringify(endpoint.options || {}));
    cell(row, endpoint.version);
    rows.appendChild(row);
  }
}

function selectEndpoint(endpoint) {
  el("endpoint-name").value = endpoint.name;
  el("endpoint-connector").value = endpoint.connector;
  el("endpoint-address").value = endpoint.address;
  el("endpoint-enabled").checked = endpoint.enabled !== false;
  el("endpoint-options").value = JSON.stringify(endpoint.options || {}, null, 2);
  say("endpoint-outcome", endpoint.name + " is at version " + endpoint.version
    + ", which a save quotes back", "");
}

// versionOf finds what a named row was last read at, which is the precondition
// its write carries. A name that is not on the list is one being created.
function versionOf(items, name) {
  const found = items.filter(function (item) { return item.name === name; });
  return found.length > 0 ? found[0].version : "";
}

async function saveEndpoint() {
  const name = el("endpoint-name").value.trim();
  if (!name) {
    say("endpoint-outcome", "a name is needed", "warn");
    return;
  }

  let options;
  try {
    options = JSON.parse(el("endpoint-options").value || "{}");
  } catch (err) {
    say("endpoint-outcome", "the options are not JSON: " + err.message, "bad");
    return;
  }

  const request = preconditionFor({ version: versionOf(configuration.endpoints, name) });
  request.body = {
    connector: el("endpoint-connector").value,
    address: el("endpoint-address").value.trim(),
    enabled: el("endpoint-enabled").checked,
    options: options,
  };

  const answer = await apiCall("PUT",
    "/api/v1/connector-endpoints/" + encodeURIComponent(name), request);

  await reloadOverlay();

  report("endpoint-outcome", answer,
    answer.status + ", written. The dial loop picks it up without a restart.");
}

async function deleteEndpoint() {
  const name = el("endpoint-name").value.trim();
  const version = versionOf(configuration.endpoints, name);

  if (!version) {
    say("endpoint-outcome", "no endpoint with that name has been read, so there is nothing to delete", "warn");
    return;
  }

  const answer = await apiCall("DELETE",
    "/api/v1/connector-endpoints/" + encodeURIComponent(name), { ifMatch: '"' + version + '"' });

  await reloadOverlay();

  report("endpoint-outcome", answer, "deleted " + name);
}

// ── saved gcode sequences ────────────────────────────────────────────────────

async function loadPresets() {
  const answer = await apiCall("GET", "/api/v1/gcode-presets");
  const rows = body("presets");
  clear(rows);

  if (!answer.ok) {
    say("preset-outcome", answer.status + " " + answer.detail, "bad");
    return;
  }

  configuration.presets = (answer.body && answer.body.items) || [];

  // The printers area runs one of these against the selected printer, so the
  // picker there is filled from the same read.
  fill("gcode-preset", configuration.presets.map(function (preset) { return preset.name; }),
    "pick a sequence");

  for (const preset of configuration.presets) {
    const row = document.createElement("tr");
    row.className = "selectable";
    row.addEventListener("click", function () { selectPreset(preset); });
    cell(row, preset.name);
    cell(row, preset.summary);
    cell(row, (preset.models || []).join(" ") || "every model");
    cell(row, (preset.steps || []).length);
    cell(row, preset.force ? "yes" : "no");
    cell(row, preset.version);
    rows.appendChild(row);
  }
}

function selectPreset(preset) {
  el("preset-name").value = preset.name;
  el("preset-summary").value = preset.summary || "";
  el("preset-models").value = (preset.models || []).join(", ");
  el("preset-steps").value = (preset.steps || []).join("\n");
  el("preset-force").checked = preset.force === true;
  say("preset-outcome", preset.name + " is at version " + preset.version, "");
}

async function savePreset() {
  const name = el("preset-name").value.trim();
  if (!name) {
    say("preset-outcome", "a name is needed", "warn");
    return;
  }

  const request = preconditionFor({ version: versionOf(configuration.presets, name) });
  request.body = {
    summary: el("preset-summary").value,
    models: commas(el("preset-models").value),
    steps: lines(el("preset-steps").value),
    force: el("preset-force").checked,
  };

  const answer = await apiCall("PUT",
    "/api/v1/gcode-presets/" + encodeURIComponent(name), request);

  await reloadOverlay();

  report("preset-outcome", answer, answer.status + ", written");
}

async function deletePreset() {
  const name = el("preset-name").value.trim();
  const version = versionOf(configuration.presets, name);

  if (!version) {
    say("preset-outcome", "no sequence with that name has been read, so there is nothing to delete", "warn");
    return;
  }

  const answer = await apiCall("DELETE",
    "/api/v1/gcode-presets/" + encodeURIComponent(name), { ifMatch: '"' + version + '"' });

  await reloadOverlay();

  report("preset-outcome", answer, "deleted " + name);
}

// ── what this build says about itself ────────────────────────────────────────

async function loadConnectorHealth() {
  const answer = await apiCall("GET", "/api/v1/health/connectors");
  if (!answer.ok) { return; }

  const rows = body("connector-health");
  clear(rows);

  for (const rollup of (answer.body && answer.body.items) || []) {
    const row = document.createElement("tr");
    cell(row, rollup.connector);
    cell(row, rollup.total);
    cell(row, JSON.stringify(rollup.counts || {}));
    cell(row, (rollup.degraded || []).length
      + ((rollup.degraded_truncated || 0) > 0 ? " and " + rollup.degraded_truncated + " more" : ""),
      rollup.status === "ok" ? "ok" : "warn");
    rows.appendChild(row);
  }
}

async function loadNamespaces() {
  const answer = await apiCall("GET", "/api/v1/state/namespaces");
  if (!answer.ok) { return; }

  const rows = body("namespaces");
  clear(rows);

  for (const namespace of (answer.body && answer.body.items) || []) {
    const row = document.createElement("tr");
    cell(row, namespace.namespace);
    cell(row, namespace.instance);
    cell(row, namespace.keys);
    cell(row, namespace.bytes);
    rows.appendChild(row);
  }
}

async function loadVocabularies() {
  const index = await readJSON("/api/v1/vocabularies");

  const rows = body("vocabularies");
  clear(rows);

  for (const entry of index.items || []) {
    const row = document.createElement("tr");
    cell(row, entry.name);
    cell(row, entry.version);
    cell(row, entry.format);
    cell(row, entry.summary);
    rows.appendChild(row);
  }
}

function wireConfig() {
  el("resource-save").addEventListener("click", saveResource);
  el("resource-restart").addEventListener("click", restartResource);
  el("resource-delete").addEventListener("click", deleteResource);
  el("resource-new").addEventListener("click", startNewResource);
  el("endpoint-save").addEventListener("click", saveEndpoint);
  el("endpoint-delete").addEventListener("click", deleteEndpoint);
  el("preset-save").addEventListener("click", savePreset);
  el("preset-delete").addEventListener("click", deletePreset);
}

async function loadConfiguration() {
  const jobs = [
    ["the resource instances", loadResources()],
    ["the resource kinds", loadResourceKinds()],
    ["the connector endpoints", loadEndpoints()],
    ["the gcode presets", loadPresets()],
    ["connector health", loadConnectorHealth()],
    ["the state namespaces", loadNamespaces()],
    ["the vocabulary index", loadVocabularies()],
  ];

  for (const [what, job] of jobs) {
    try {
      await job;
    } catch (err) {
      console.error("loading " + what, err);
    }
  }
}
