// The first-run area: what a person does to a server that has just been
// installed, in the order they have to do it.
//
// Every step is an ordinary operation, and the checklist above them is read from
// the running server rather than from a flag this page keeps. So a step done
// from a terminal, from another browser or from a configuration file shows as
// done here, and a console that was open while somebody else finished the
// install does not have to be told.

// authenticates reports whether this deployment identifies anybody at all. A
// deployment with neither a password login nor an issuer serves every request as
// one subject, which is a different thing from one that has been set up.
function authenticates(info) {
  return info.credentials === true || info.redirect === true;
}

// setupChecks is the whole checklist: what to look at, and what a reader does
// about a row that is not done. Each check is handed the four reads the area
// makes and answers a state and a sentence.
function setupChecks(info, principal, readiness, fleet, accounts) {
  return [
    {
      step: "an administrator exists",
      done: info.needs_setup === false,
      // A deployment that authenticates nobody has no account store, so there
      // is no administrator to create and none missing. Reporting it as "the
      // setup path is closed" would send a reader looking for the account that
      // closed it, and there is not one.
      note: !authenticates(info)
        ? "This deployment authenticates nobody, so every request is served as one subject and there "
          + "is no account to create. Configure an identity provider or local accounts to change that."
        : info.needs_setup
          ? "No account exists, so the setup path below is open and anybody who can reach this server "
            + "may take it. This is the step to do first."
          : "The setup path is closed. " + accounts + " accounts exist, and a new one is created under "
            + "users and access rather than here.",
    },
    {
      step: "this browser is somebody",
      done: principal !== "",
      note: principal
        ? "Every request from this page is served as " + principal + "."
        : "This request carries no identity the server accepts, so nothing below will be permitted.",
    },
    {
      step: "somewhere to keep files",
      done: (readiness.instances || []).some(function (instance) {
        // The verdict names an instance as kind:name, which is the shape the
        // readiness payload documents. Matching kind/name instead reported
        // every server as having no storage at all.
        return instance.resource.indexOf("storage:") === 0 && instance.serving;
      }),
      done_note: "A storage instance is serving, so an upload has somewhere to land.",
      // The verdict counts every declared instance and lists only the ones this
      // caller may see, so an empty list under a positive count is a permission
      // and not an empty server. Reporting the two the same way would send
      // somebody to declare a storage that is already there.
      note: (readiness.instances || []).length === 0 && (readiness.declared || 0) > 0
        ? "This caller may not see the " + readiness.declared + " declared instances, so whether one of "
          + "them is a storage cannot be read from here. Log in as somebody who holds view_resource."
        : "No storage instance is serving. Step three declares one on the local filesystem.",
    },
    {
      step: "every required instance is serving",
      done: readiness.ready === true,
      note: readiness.ready
        ? readiness.serving + " of " + readiness.declared + " declared instances are serving."
        : "The server is not ready, and it is blocked on " + ((readiness.blocked || []).join(", ")
          || "an instance it did not name") + ". Configuration is where those are fixed.",
    },
    {
      step: "a printer has arrived",
      done: fleet > 0,
      note: fleet > 0
        ? fleet + " printers have dialled in and been admitted."
        : "No printer has dialled in. A printer this server dials is declared in step two; a printer that "
          + "dials in needs nothing declared, and appears under printers once its firmware points here.",
    },
  ];
}

async function loadSetup() {
  const info = await loadAuthInfo();
  const principal = await loadPrincipal();
  const readiness = await loadReadiness();

  let fleet = 0;
  const listing = await apiCall("GET", "/api/v1/devices?limit=1");
  if (listing.ok && listing.body) { fleet = Number(listing.body.total) || 0; }

  const rows = body("setup-checklist");
  clear(rows);

  for (const check of setupChecks(info, principal, readiness, fleet, access.accounts.length)) {
    const row = document.createElement("tr");
    cell(row, check.step);
    cell(row, check.done ? "done" : "not yet", check.done ? "ok" : "warn");
    cell(row, check.done && check.done_note ? check.done_note : check.note);
    rows.appendChild(row);
  }

  say("setup-state", !authenticates(info)
    ? "This deployment authenticates nobody, so it has no account store and the form below has nothing "
      + "to create. Every request is already served as one subject."
    : info.needs_setup
      ? "This server has no account. The form below creates the one the first grant already names."
      : "This server already has an account, so the setup operation is closed and answers 409.",
    info.needs_setup ? "warn" : "");

  el("setup-submit").disabled = info.needs_setup === false;
}

async function createFirstAdministrator() {
  const answer = await apiCall("POST", "/api/v1/auth/setup", {
    body: {
      username: el("setup-username").value,
      display_name: el("setup-display").value,
      password: el("setup-password").value,
    },
  });

  if (!answer.ok) {
    say("setup-outcome", answer.status + " " + answer.detail, "bad");
    return;
  }

  say("setup-outcome", "created " + answer.body.id
    + ", who holds this server's first grant. Log in as them next.", "ok");

  // The credentials go across to the login form, so the step after this one is
  // one button rather than the same two fields typed a second time.
  el("login-username").value = el("setup-username").value;
  el("login-password").value = el("setup-password").value;

  await loadSetup();
}

// loginAsAdministrator finishes the first step. The setup operation creates the
// account and hands back no credential, so a person who stopped there has an
// administrator they are not logged in as.
async function loginAsAdministrator() {
  el("login-username").value = el("setup-username").value;
  el("login-password").value = el("setup-password").value;

  const done = await login();

  say("setup-outcome", done
    ? "logged in as " + el("setup-username").value + ". The password fields are cleared."
    : "the login was refused, and the reason is under users and access", done ? "ok" : "bad");

  el("setup-password").value = "";

  await loadSetup();
}

async function declareFirstPrinter() {
  const name = el("setup-printer-name").value.trim();

  if (!name) {
    say("setup-printer-outcome", "a name is needed", "warn");
    return;
  }

  const request = preconditionFor({ version: versionOf(configuration.endpoints, name) });
  request.body = {
    connector: el("setup-printer-connector").value,
    address: el("setup-printer-address").value.trim(),
    enabled: true,
    options: {},
  };

  const answer = await apiCall("PUT",
    "/api/v1/connector-endpoints/" + encodeURIComponent(name), request);

  await reloadOverlay();
  await loadSetup();

  report("setup-printer-outcome", answer,
    answer.status + ", declared. The dial loop starts on it, and the printer appears under printers "
    + "once the handshake succeeds.");
}

async function declareFirstStorage() {
  const name = el("setup-storage-name").value.trim();
  const root = el("setup-storage-root").value.trim();

  if (!name || !root) {
    say("setup-storage-outcome", "a name and a directory are both needed", "warn");
    return;
  }

  const existing = configuration.resources.filter(function (instance) {
    return instance.kind === "storage" && instance.name === name;
  });

  const request = preconditionFor({ version: existing.length > 0 ? existing[0].version : "" });
  request.body = {
    type: "filesystem",
    enabled: true,
    required: el("setup-storage-required").checked,
    config: { root: root },
  };

  const answer = await apiCall("PUT",
    "/api/v1/resources/storage/" + encodeURIComponent(name), request);

  await reloadOverlay();
  await loadSetup();

  report("setup-storage-outcome", answer,
    answer.status + ", declared. The supervisor starts it, and readiness says when it is serving.");
}

function wireSetup() {
  el("setup-submit").addEventListener("click", createFirstAdministrator);
  el("setup-login").addEventListener("click", loginAsAdministrator);
  el("setup-printer-save").addEventListener("click", declareFirstPrinter);
  el("setup-storage-save").addEventListener("click", declareFirstStorage);
}
