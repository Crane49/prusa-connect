// The users and access area: who this browser is, the accounts this server
// holds, and the authorization graph.
//
// The graph is the product's heart and until this page it was exercised only
// from Go tests. Everything here goes through the published operations, so a
// grant written on this screen is the same write a client makes.

const access = {
  accounts: [],
  selectedAccount: null,
  // vocabulary is the authorization set the server publishes. The pickers are
  // built from it rather than from a list spelled here, so a relation added to
  // the graph appears on this screen without the page being changed.
  vocabulary: null,
  // grantScope is the object the grants table was last read for, so a withdraw
  // acts on the object the reader is looking at and not the one in the field.
  grantScope: null,
};

// ── who this browser is ──────────────────────────────────────────────────────

async function loadPrincipal() {
  const answer = await apiCall("GET", "/api/v1/principal");

  const who = answer.ok && answer.body ? answer.body.principal : "";
  el("principal").textContent = "identity: " + (who || "none (" + answer.status + ")");
  el("principal").className = "pill " + (who ? "ok" : "bad");

  say("identity-principal", who
    ? "The authorization graph keys this caller as " + who
      + ". Every grant on this screen is read and written as them."
    : "This request carries no identity the server accepts, and it answered " + answer.status
      + ". Nothing below will read until a login succeeds.",
    who ? "ok" : "bad");

  return who;
}

async function loadAuthInfo() {
  const answer = await apiCall("GET", "/api/v1/auth/info");
  const info = answer.body || {};

  const how = [];
  if (info.credentials) { how.push("a username and password are accepted here"); }
  if (info.redirect) { how.push("a login starts by sending the browser to " + (info.provider || "an issuer")); }
  if (how.length === 0) { how.push("this deployment authenticates nobody, so every request is served as one subject"); }
  if (info.needs_setup) { how.push("no account exists yet, so the first-run screen is open"); }

  say("identity-info", how.join(". "), info.needs_setup ? "warn" : "");

  return info;
}

async function login() {
  const answer = await apiCall("POST", "/api/v1/auth/login", {
    body: { username: el("login-username").value, password: el("login-password").value },
  });

  if (!answer.ok) {
    say("login-outcome", answer.status + " " + answer.detail, "bad");
    return false;
  }

  el("login-password").value = "";
  say("login-outcome", "logged in, and the server stops accepting this credential at "
    + (answer.body && answer.body.expires_at), "ok");

  await afterIdentityChanged();

  return true;
}

async function logout() {
  const answer = await apiCall("POST", "/api/v1/auth/logout", { body: {} });
  say("login-outcome", answer.ok ? "logged out" : answer.status + " " + answer.detail,
    answer.ok ? "ok" : "bad");

  await afterIdentityChanged();
}

// afterIdentityChanged re-reads everything a login moves. A console that showed
// the previous identity's fleet after a login is one that cannot be used to test
// what a grant does, which is the whole reason this screen exists.
async function afterIdentityChanged() {
  await loadPrincipal();
  await loadAuthInfo();
  await loadAccounts();
  await refresh();
  openStream();
}

// ── accounts ─────────────────────────────────────────────────────────────────

async function loadAccounts() {
  const answer = await apiCall("GET", "/api/v1/auth/accounts");
  const rows = body("accounts");
  clear(rows);

  if (!answer.ok) {
    access.accounts = [];
    say("accounts-outcome", "the account list answered " + answer.status + " " + answer.detail
      + ". Reading it takes configure_resource on the auth resource kind.", "bad");

    return;
  }

  access.accounts = (answer.body && answer.body.items) || [];
  say("accounts-outcome", access.accounts.length + " accounts", "ok");

  for (const account of access.accounts) {
    const row = document.createElement("tr");
    row.className = "selectable" + (account.id === access.selectedAccount ? " selected" : "");
    row.addEventListener("click", function () {
      access.selectedAccount = account.id;
      el("grant-subject").value = "user:" + account.id;
      loadAccounts();
    });
    cell(row, account.id);
    cell(row, account.username);
    cell(row, account.display_name);
    cell(row, account.created_at);
    cell(row, account.id === access.selectedAccount ? "selected" : "");
    rows.appendChild(row);
  }
}

async function createAccount() {
  const answer = await apiCall("POST", "/api/v1/auth/accounts", {
    body: {
      username: el("account-username").value,
      display_name: el("account-display").value,
      password: el("account-password").value,
    },
  });

  if (!answer.ok) {
    say("accounts-outcome", answer.status + " " + answer.detail, "bad");
    return;
  }

  el("account-password").value = "";
  access.selectedAccount = answer.body.id;

  // The list is re-read before the outcome is written and not after. A reload
  // reports what it read, so a console that said what it had done first would
  // have that sentence replaced by "3 accounts" a moment later, and every write
  // on this screen would look like it had done nothing.
  await loadAccounts();

  say("accounts-outcome", "created " + answer.body.id
    + ", which holds no grant yet, so it can log in and reach nothing", "ok");
}

async function deleteAccount() {
  if (!access.selectedAccount) {
    say("accounts-outcome", "pick an account on the list first", "warn");
    return;
  }

  const removed = access.selectedAccount;
  const answer = await apiCall("DELETE",
    "/api/v1/auth/accounts/" + encodeURIComponent(removed));

  if (answer.ok) { access.selectedAccount = null; }

  await loadAccounts();

  say("accounts-outcome", answer.ok ? "deleted " + removed
    : answer.status + " " + answer.detail, answer.ok ? "ok" : "bad");
}

async function setAccountPassword() {
  if (!access.selectedAccount) {
    say("accounts-outcome", "pick an account on the list first", "warn");
    return;
  }

  const answer = await apiCall("POST",
    "/api/v1/auth/accounts/" + encodeURIComponent(access.selectedAccount) + "/password",
    { body: { password: el("account-password").value } });

  el("account-password").value = "";
  say("accounts-outcome", answer.ok
    ? "the password is set, and every session opened under the old one has ended"
    : answer.status + " " + answer.detail, answer.ok ? "ok" : "bad");
}

// ── the authorization vocabulary ─────────────────────────────────────────────

async function loadAuthz() {
  const vocabulary = await readJSON("/api/v1/vocabularies/authorization");
  access.vocabulary = vocabulary;

  say("authz-version", "version " + vocabulary.version + ", entries in the "
    + vocabulary.format + " shape", "");

  const objects = (vocabulary.types || []).filter(function (type) { return type.object; })
    .map(function (type) { return type.type; });

  fill("grant-object-type", objects, "pick a type");
  // The access check answers on the types its own body accepts, which is a
  // narrower set than the graph writes grants on.
  fill("check-object-type", ["device", "resource_kind"], "pick a type");

  const relations = body("authz-relations");
  clear(relations);
  for (const relation of vocabulary.relations || []) {
    const row = document.createElement("tr");
    cell(row, relation.relation);
    cell(row, relation.role ? "confers actions" : "carries what it names");
    cell(row, relation.summary);
    relations.appendChild(row);
  }

  const actions = body("authz-actions");
  clear(actions);
  for (const act of vocabulary.actions || []) {
    const row = document.createElement("tr");
    cell(row, act.action);
    cell(row, act.domain);
    cell(row, (act.conferred_by || []).join(" "));
    cell(row, act.summary);
    actions.appendChild(row);
  }

  const shapes = body("authz-shapes");
  clear(shapes);
  for (const shape of vocabulary.shapes || []) {
    const row = document.createElement("tr");
    cell(row, shape.object_type);
    cell(row, shape.relation);
    cell(row, shape.subject_type);
    cell(row, shape.subject_relation);
    shapes.appendChild(row);
  }

  narrowGrantRelations();
  narrowCheckActions();
}

// narrowGrantRelations offers the relations that are well formed on the object
// type in the field. A tuple whose relation the object type does not carry is
// refused by the server, and offering it here is offering a refusal.
function narrowGrantRelations() {
  const type = el("grant-object-type").value;
  const shapes = (access.vocabulary && access.vocabulary.shapes) || [];

  const relations = Array.from(new Set(shapes
    .filter(function (shape) { return !type || shape.object_type === type; })
    .map(function (shape) { return shape.relation; }))).sort();

  fill("grant-relation", relations, "pick a relation");
}

function narrowCheckActions() {
  const type = el("check-object-type").value;
  const actions = ((access.vocabulary && access.vocabulary.actions) || [])
    .filter(function (act) { return !type || act.domain === type; })
    .map(function (act) { return act.action; }).sort();

  fill("check-action", actions, "pick an action");
}

// ── grants ───────────────────────────────────────────────────────────────────

function grantPath(type, id) {
  return "/api/v1/grants/" + encodeURIComponent(type) + "/" + encodeURIComponent(id);
}

async function loadGrants() {
  const type = el("grant-object-type").value;
  const id = el("grant-object-id").value.trim();

  const rows = body("grants");

  if (!type || !id) {
    clear(rows);
    say("grants-outcome", "name an object type and an object id first", "warn");
    return;
  }

  const answer = await apiCall("GET", grantPath(type, id));
  clear(rows);

  if (!answer.ok) {
    say("grants-outcome", answer.status + " " + answer.detail
      + ". Reading grants takes the same grant that writing one takes on this object.", "bad");
    return;
  }

  access.grantScope = { type: type, id: id };
  const items = (answer.body && answer.body.items) || [];

  say("grants-outcome", items.length === 0
    ? "no grants are written on " + type + ":" + id + ", and this caller may read them"
    : items.length + " grants on " + type + ":" + id, "ok");

  for (const grant of items) {
    const row = document.createElement("tr");
    cell(row, grant.object);
    cell(row, grant.relation);
    cell(row, grant.subject);
    action(row, "withdraw", function () { withdrawGrant(grant); });
    rows.appendChild(row);
  }
}

async function writeGrant() {
  const type = el("grant-object-type").value;
  const id = el("grant-object-id").value.trim();
  const relation = el("grant-relation").value;
  const subject = el("grant-subject").value.trim();

  if (!type || !id || !relation || !subject) {
    say("grants-outcome", "an object type, an object id, a relation and a subject are all needed", "warn");
    return;
  }

  const answer = await apiCall("POST", grantPath(type, id),
    { body: { relation: relation, subject: subject } });

  if (answer.ok) { await loadGrants(); }

  say("grants-outcome", answer.ok
    ? "wrote " + relation + " on " + type + ":" + id + " for " + subject
      + ", and the authorization epoch has moved"
    : answer.status + " " + answer.detail, answer.ok ? "ok" : "bad");
}

async function withdrawGrant(grant) {
  const scope = access.grantScope;
  if (!scope) { return; }

  const answer = await apiCall("DELETE", grantPath(scope.type, scope.id)
    + "?relation=" + encodeURIComponent(grant.relation)
    + "&subject=" + encodeURIComponent(grant.subject));

  await loadGrants();

  say("grants-outcome", answer.ok
    ? "withdrew " + grant.relation + " from " + grant.subject
      + ". A withdrawal answers the same whether or not the graph held it"
    : answer.status + " " + answer.detail, answer.ok ? "ok" : "bad");
}

// ── what this caller may do ──────────────────────────────────────────────────

async function checkAccess() {
  const answer = await apiCall("POST", "/api/v1/access/check", {
    body: {
      object_type: el("check-object-type").value,
      object_id: el("check-object-id").value.trim(),
      action: el("check-action").value,
    },
  });

  const list = el("check-actions");
  clear(list);

  if (!answer.ok) {
    say("check-outcome", answer.status + " " + answer.detail, "bad");
    return;
  }

  const decision = answer.body || {};

  say("check-outcome", (decision.allowed ? "allowed" : "refused")
    + " at authorization epoch " + decision.authorization_epoch
    + ". An object this server does not have answers exactly as one this caller reaches nothing on.",
    decision.allowed ? "ok" : "warn");

  for (const act of decision.actions || []) {
    const item = document.createElement("li");
    item.textContent = act;
    list.appendChild(item);
  }
}

function wireAccess() {
  el("login-submit").addEventListener("click", login);
  el("logout").addEventListener("click", logout);
  el("account-create").addEventListener("click", createAccount);
  el("account-delete").addEventListener("click", deleteAccount);
  el("account-password-set").addEventListener("click", setAccountPassword);
  el("grants-load").addEventListener("click", loadGrants);
  el("grant-write").addEventListener("click", writeGrant);
  el("grant-object-type").addEventListener("change", narrowGrantRelations);
  el("check-object-type").addEventListener("change", narrowCheckActions);
  el("check-run").addEventListener("click", checkAccess);
}

async function loadAccess() {
  const jobs = [
    ["the principal", loadPrincipal()],
    ["the login this deployment serves", loadAuthInfo()],
    ["the authorization vocabulary", loadAuthz()],
    ["the accounts", loadAccounts()],
  ];

  for (const [what, job] of jobs) {
    try {
      await job;
    } catch (err) {
      console.error("loading " + what, err);
    }
  }
}
