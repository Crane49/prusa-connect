---
title: Local accounts
description: Log in to a self-hosted server that has no identity provider. Create the first administrator, add people, and change a password.
audience: run
rewrite:
  describes: current
  implementedBy:
    - internal/platform/auth/local
    - internal/app/accounts.go
    - internal/app/authsetup.go
    - internal/transport/http/httpapi/accounts.go
    - internal/transport/http/httpapi/setup.go
    - internal/transport/http/loginguard
---

A workshop with three printers is not going to run an identity provider. This
server holds the accounts itself when you tell it to, so a self-hoster with no
issuer can still log in. It is one configuration key and one call.

Turn it on, create the first administrator, and that person is the
administrator of this server. Everybody else is an account they add.

## The first run

A new install has decided nothing about who may call it. Until somebody
decides, it answers four things and refuses everything else with
`core.auth/SETUP_PENDING` and status 503:

| path | why it answers |
|---|---|
| `GET /api/v1/health` | a watchdog sends it whatever the process is doing |
| `GET /api/v1/readiness` | a load balancer routes on it and holds no credential |
| `/` | the page that takes the decision |
| `/api/v1/setup` | the call that records it |

Ask what is left to do:

```bash
curl -s http://localhost:8080/api/v1/setup
```

```json
{"state":"pending","version":"0.5.0","issuer":false,"modules":[],
 "steps":[{"step":"mode","done":false,"detail":"Nothing has been recorded about identity yet."},
          {"step":"administrator","done":false,"detail":"No account has been created yet."},
          {"step":"admission","done":false,"detail":"Nothing has been recorded about how a new printer joins."}]}
```

`modules` lists the modules this install declares and whether each is switched
on, so one call can switch them on with the decision.

`steps` is what the first run has done, one row per step, read from what is on
disk rather than from what the running process remembers. A person who closes
the browser halfway resumes where they stopped, and a server set up long ago
still reports what its first run decided. The keys are `mode`,
`administrator`, `admission`, and `module:` followed by the instance.

Recording the decision takes the setup code the server printed in its log
when it started, in the `code` field. Read it in the terminal, with
`docker logs` for a container, or with `journalctl -u` for a service. A
restart prints a new code. `core.auth.setup_code` sets a code of your own
instead.

Record the decision. There are two:

| mode | what it means |
|---|---|
| `open` | this server asks for no credential. Every request is served as `core.principal` |
| `required` | every caller presents a credential. The call creates the first administrator |

`required` takes an account and answers with that person's session, so the page
carries on as them:

```bash
curl -s -c cookies.txt -X POST http://localhost:8080/api/v1/setup   -H 'content-type: application/json'   -d '{"code":"K7QD-3M9X-TP2A","mode":"required","account":{"username":"me@example.com","password":"a password of at least eight bytes"}}'
```

```json
{"state":"done","mode":"required","admission":"ask","signed_in":true,
 "steps":[{"step":"mode","done":true,"detail":"Every caller presents a credential."},
          {"step":"administrator","done":true,"detail":"This server holds an account to sign in with."},
          {"step":"admission","done":true,"detail":"A new printer shows a code and waits for you to approve it."}]}
```

`open` takes no account, because a password on a server that asks for none is
one nothing would ever check:

```bash
curl -s -X POST http://localhost:8080/api/v1/setup   -H 'content-type: application/json' -d '{"code":"K7QD-3M9X-TP2A","mode":"open"}'
```

The choice is written to `auth.mode` in the machine-owned file beside your
configuration, `config.d/resources.json`. Read it back there, or at
`GET /api/v1/setup`. A second call answers 409: reopening the first run over
the API would let whoever reaches the port choose again.

### How a new printer joins

The same call takes `admission`, which says what happens when a printer this
server has never seen dials in:

| admission | what it means |
|---|---|
| `ask` | the printer registers, shows a code on its screen, and you approve that code. It is what an absent key takes |
| `open` | the printer is let in the moment it dials, which is what a wire you trust means |

```bash
curl -s -X POST http://localhost:8080/api/v1/setup   -H 'content-type: application/json' -d '{"code":"K7QD-3M9X-TP2A","mode":"open","admission":"open"}'
```

It is written to `admission.mode` in the same machine-owned file, beside
`auth.mode`, and `GET /api/v1/setup` reports it back.

The connector is what acts on it. `core.connectors` is your own file and this
server never writes it. So the shipping xBuddy dialect reads its `mode` key
from your block where you wrote one. Where you wrote none, it reads this
recorded choice: `ask` is `mode: token` and `open` is `mode: none`. A connector's mode
is fixed when the driver is built, so a choice recorded while the server runs
takes effect at the next start. A dialect this build cannot translate for is
left exactly as its own configuration has it.

Nothing is recorded until the whole call succeeds. An account that could not be
created leaves the install pending, and the page asks again.

### An install that already runs

A server set up before this key existed keeps working. The first start that
looks for the key writes the decision that install already took, and says so in
a log line:

| what the install has | recorded |
|---|---|
| `core.auth.issuer` | `required` |
| `core.auth.local` | `required` |
| accounts in its database | `required` |
| a `core.principal` the operator chose | `open` |
| none of those | nothing. The install is a first run |

## Configuration

```yaml
core:
  auth:
    local: true
    # accounts: config/config.d/accounts.json
```

| key | required | default | description |
|---|---|---|---|
| `core.auth.local` | no | `false` | serve a password login against this server's own accounts |
| `core.auth.accounts` | no | `config.d/accounts.json` | an account file from an earlier build, copied into the database once |
| `core.principal` | no | `operator` | the id the first administrator is created under |
| `core.trusted_proxies` | no | empty | the networks a reverse proxy reaches this server from, which is what the login limit counts by |

`core.auth.local` and `core.auth.issuer` cannot both be set. One deployment
serves one identity provider, and a configuration naming two is refused at
startup rather than resolved at runtime. A workshop that later buys a
company login migrates its people across rather than running both.

The accounts are rows in this server's database, beside the grants, so one
backup holds both. Each row holds an argon2id password hash and never the
password. An earlier build kept the accounts in a file. The server copies that
file into the database when the database holds no account, and never reads it
again.

## Endpoints

| method | path | purpose |
|---|---|---|
| `GET` | `/api/v1/setup` | whether this install has decided who may call it |
| `POST` | `/api/v1/setup` | record that decision, and create the administrator |
| `GET` | `/api/v1/auth/info` | which login this server serves, and whether it has been set up |
| `POST` | `/api/v1/auth/setup` | create the first administrator |
| `POST` | `/api/v1/auth/login` | log in and receive the session cookie |
| `POST` | `/api/v1/auth/logout` | end this session |
| `GET` | `/api/v1/auth/accounts` | list the people on this server |
| `POST` | `/api/v1/auth/accounts` | add a person |
| `DELETE` | `/api/v1/auth/accounts/{account}` | remove a person |
| `POST` | `/api/v1/auth/accounts/{account}/password` | change or reset a password |

The first five take no credential. Nobody has one on a first run, and the
routes that hand one out cannot need one.

## Create the first administrator

Start the server with `core.auth.local: true`. Until an administrator exists,
the server stays in its first run and prints the setup code in its log. The
web app's first-run page, or `POST /api/v1/setup` with mode `required`,
creates the administrator with that code. Mode `open` is refused, because the
file asks every caller for a password.

A server that recorded its decision before it had an account answers the path
below instead. It takes the same setup code, which the server prints at start
while no account exists.

```bash
curl -s http://localhost:8080/api/v1/auth/info
```

```json
{"provider":"local","credentials":true,"redirect":false,"needs_setup":true}
```

Create the account:

```bash
curl -s -X POST http://localhost:8080/api/v1/auth/setup \
  -H 'content-type: application/json' \
  -d '{"code":"K7QD-3M9X-TP2A","username":"me@example.com","display_name":"Me","password":"a password of at least eight bytes"}'
```

Then log in. The cookie file is where the session credential lands.

```bash
curl -s -c cookies.txt -X POST http://localhost:8080/api/v1/auth/login \
  -H 'content-type: application/json' \
  -d '{"username":"me@example.com","password":"a password of at least eight bytes"}'
```

Every call afterwards sends that cookie:

```bash
curl -s -b cookies.txt http://localhost:8080/api/v1/resources
```

## Why the first account is the administrator

The authorization graph writes its first grant on a first run, for the subject
named by `core.principal`. Setup creates the account under that same id, so the
person who runs it holds what that grant already confers: admin of `org:local`,
which is manager on every resource kind.

There is no second bootstrap door. Setup writes no grant of its own, and once
an account exists it is closed. Two calls arriving at once create one account:
the id is fixed, so the second write has nowhere to go.

Deleting the last account is refused for the same reason. An empty store is the
first-run state, and reopening it would hand this server to whoever asked next.

## Add people

A new account holds no grant. That person can log in and can do nothing until
somebody writes a grant for them. Who is on this server and what they may do
stay two separate answers.

```bash
curl -s -b cookies.txt -X POST http://localhost:8080/api/v1/auth/accounts \
  -H 'content-type: application/json' \
  -d '{"username":"colleague@example.com","password":"another password"}'
```

The response carries the account's `id`. Grant them something with it:

```bash
curl -s -b cookies.txt -X POST http://localhost:8080/api/v1/grants/resource_kind/storage \
  -H 'content-type: application/json' \
  -d '{"relation":"viewer","subject":"user:u4k2p9z1m7x3c8v5b0nq"}'
```

### Access to one printer

An organization role reaches every printer. To change what one person may do
on one printer, an organization administrator uses the web app in one of two
places. The printer's page has an Access card on its Printer tab. The person's
page lists every printer, and you open it from Settings, People.

Each row has an access switch and a role.

- **Access off** writes a `blocked` grant. A block takes away every level the
  person holds on that printer, whatever gave it.
- **The role** is "As in organization", Viewer, Operator or Administrator. A
  printer role can only add to the organization role. A grant at or below the
  organization role has no effect, and the row says so.

The same grants through the API:

```bash
curl -s -b cookies.txt -X POST http://localhost:8080/api/v1/grants/device/<printer id> \
  -H 'content-type: application/json' \
  -d '{"relation":"blocked","subject":"user:u4k2p9z1m7x3c8v5b0nq"}'
curl -s -b cookies.txt \
  http://localhost:8080/api/v1/grants/subject/user:u4k2p9z1m7x3c8v5b0nq/direct
```

The second call lists the grants written for that person on every printer you
administer.

Administering accounts takes `configure_resource` on `resource_kind:auth`. The
accounts are that resource kind's data, so the permission to configure the kind
is the permission to say who logs in. The graph answers it, and there is no
second vocabulary for identity. See the
[authorization vocabulary](./authorization-vocabulary.md).

An act that creates or grants access also takes a sign-in. A sign-in is the
cookie a login returns, or a Prusa Account token that a pairing names a person
with. A token paired to a service account is not a sign-in. These
acts are adding or removing a person, resetting a password, setting a role,
writing a pairing and creating a token for somebody else. An API token is
refused them with `403 core.auth/SESSION_REQUIRED`, so a stolen token cannot
make itself a way back in.

Listing accounts, pairings and other people's tokens, and revoking a token or
a pairing, take the grant alone. A script holding an administrator's API token
can still clean up after an incident.

## Change a password

Your own password takes the current one:

```bash
curl -s -b cookies.txt -X POST http://localhost:8080/api/v1/auth/accounts/operator/password \
  -H 'content-type: application/json' \
  -d '{"current_password":"the old one","new_password":"the new one"}'
```

Somebody else's takes `configure_resource` on `resource_kind:auth` and no
current password. That is the reset for a person who has lost theirs.

Either one ends every credential and pairing the password could have been used
to create, on every transport:

- every session and every API token the account holds
- every API token that person created for somebody else, such as a service
  account
- every pairing that lets an outside account, such as a Prusa Account, act as
  that person
- every pairing that person wrote, whoever it names
- every token and pairing written down a chain that started with that
  person, for example a colleague's, made after this person paired their
  Prusa Account to that colleague

A cookie or a token taken from that person stops working on the next request,
and not at its expiry. So does a token that was being created while the
password changed. A token somebody else created for a service account stays,
unless it was created down a chain that started with this person.

A token created for somebody else by version 0.1.0 or earlier records nobody
as its creator, and so does a pairing written by then. A password change
cannot find them, so they stay. Revoke them yourself after a leaked password:
list the tokens of the service accounts and of the other people, and the
pairings, and revoke the ones you do not recognise.

A password change does not undo accounts or grants that somebody created with
the password. Review those too.

A person who changes their own password gets a new session cookie in the
answer, so they stay signed in.

## Recover a lost password

There is no "forgot password" link and no reset email. This server sends no
mail. Another administrator can reset the password under Settings > People, or
with the call above.

When nobody else can, run the reset as a command on the machine itself. Access
to the configuration and the database is what proves the right to do it. The
server can be running or stopped.

The command never changes the database's layout, because a server may have
it open. Right after an upgrade, the database still has the old layout, and
the command refuses. Start the new server once so it updates the database,
then run the reset. A database from a newer version is refused too. Run the
command from the same version as the server.

```bash
prusa-connect admin list --config config/config.yaml
prusa-connect admin reset-password --config config/config.yaml \
  --name me@example.com --password 'a new password of at least eight bytes'
```

`admin list` prints each username and its id. `admin reset-password` replaces
that password and ends the same things a change through the server ends. It
prints how many it ended:

```
the password of me@example.com (operator) was replaced. 1 session and 1 api credential it held were ended.
1 api credential it created for somebody else, directly or down a chain it started, ended too.
0 pairings to it, written by it or down a chain it started, ended too.
Review the accounts and grants on this server, because whoever knew the old password could have changed them.
```

On a server that holds tokens or pairings written by version 0.1.0 or earlier,
it also prints how many. Those name nobody as their creator, so the reset
cannot end the ones made for somebody else, and the line says to list and
revoke them.

A running server reads each credential from the database on every request. So
it refuses the ended ones at their next request, and needs no restart. The new
password works at once. Accounts, grants, printers and files stay as they were.

The password lands in the shell history. Clear it from there on a machine
other people use.

## Passwords

| property | value | source |
|---|---|---|
| algorithm | argon2id | [OWASP Password Storage](https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html) first choice |
| memory | 19 MiB, and 7 MiB in the printer build | `core.auth.password_memory_bytes`, refused below the settings OWASP lists |
| iterations | 2, and 5 in the printer build | `core.auth.password_passes`. OWASP lists 7 MiB with 5 passes as equal to 19 MiB with 2 |
| parallelism | 1 lane | predictable cost on a single-core Pi |
| minimum length | 8 bytes | [NIST SP 800-63B](https://pages.nist.gov/800-63-3/sp800-63b.html) section 5.1.1.2 |
| maximum length | 72 bytes | the hasher's input limit, so nothing is silently truncated |

The memory and the passes are stored inside each hash. When you change them,
every password still works. Each owner's next login stores the password again
at the new values.

Until then, a change can show who has an account. A refused login against
such an account still runs at the old values, and a name nobody holds runs
at the new values. So the time a refusal takes can show that the account
exists. This lasts until its owner logs in again.

There is no composition rule, no expiry and no forced rotation. NIST SP 800-63B
section 5.1.1.2 asks verifiers not to impose them, and a rule that makes people
write passwords down buys nothing.

## Guessing

A wrong password gets nowhere. Finding that out costs the sender a full
argon2id pass. No more than four of those run at once, or one in the printer
build. `core.auth.password_hashes_at_once` sets that number.

In front of that, `POST /api/v1/auth/login` holds the caller before it checks
anything. Every attempt is counted twice, once against the account it names and
once against the address it came from, and an attempt past the free allowance
waits. The wait doubles and then stops.

| counted by | free attempts | then |
|---|---|---|
| the account, folded the way the store folds the username | 3 | 250 ms, doubling per attempt |
| the client address, an IPv6 client by its `/64` | 12 | the same |
| both | | the longer of the two waits, and never more than 5 s |

The address count is also a queue. Attempts from one address go through one
after another, and each waits for the attempts in front of it as well as for
its own wait. An attempt that would wait longer than 5 s is refused at once
with `429`, the error code `core.auth/LOGIN_LIMITED` and a `Retry-After` header
that gives the seconds until its turn. No password is checked for it. The
account count does not queue, so its wait is never more than 5 s.

A correct password clears the account count, which is what NIST SP 800-63B
section 5.2.2 asks for on a successful authentication. It lowers the address
count to the free allowance and leaves the address queue as it is. The next
attempt from that address waits the shortest time, 250 ms. A guesser who logs
in to an account of their own between guesses gets no free attempts and no
empty queue from it.

Brute force is made impractical, no caller is banned outright, and every
refusal is recorded. Each server process keeps its own count. The count is not
tied to the storage adapter, so a deployment with shared storage does not hold
one count across processes.

Every attempt that waits, and every wrong password, is written to the server
log as a warning. The record carries the username, folded the way the store
folds it, and the client address. The password is never logged. A refused
attempt costs its sender nothing. So a run of them from one address is written
at the first, second, fourth and each doubling refusal, with the count so far.

Nobody is banned. Section 5.2.2 asks for a limit on consecutive failed
attempts, and asks that the limit not lock the legitimate subscriber out. The
three mitigations it offers for that are an allowlist, a CAPTCHA and a wait, and
the wait is the one a workshop can run. A refusal lasts until the address's
turn comes, which is never more than 5 s away. Only the address is ever
refused, so somebody who knows your username can make you wait five seconds,
and cannot keep you out from another address. A workshop behind one address
stays usable while somebody there is guessing: one person getting in brings
everybody on it back to the shortest wait. While a guesser on that address keeps its
queue full, the others on it get `429` too, and get in once they retry after
the wait.

What that buys is a rate. One address gets about one attempt every five
seconds on any account, however many connections it opens. A guesser with many
addresses gets that rate per address, and all of them together are still
bounded by the argon2id passes that may run at once.

The two counts are held in two tables of at most 4096 keys each, about 1.6 MiB
together. A caller picks the username they send, and behind a proxy the address
as well, so the tables are capped. A full table counts every key it does not
already hold under one shared overflow key, until its entries age out. The
overflow key waits and never queues, so a caller who fills the address table
cannot keep out the addresses it could not record. A full table evicts nothing
to make room: evicting is the way to erase the count on the account being
guessed at.

### The client address

The count is only as good as the address behind it, and there is one key to get
right.

| deployment | `core.trusted_proxies` | what is counted |
|---|---|---|
| reached directly | empty, which is the default | the address the socket reports |
| behind a reverse proxy | the networks the proxy reaches this server from | the rightmost `X-Forwarded-For` entry that is not one of those networks |

`X-Forwarded-For` from a peer that is not declared is not read at all, because
any client can write one. Leaving the key empty behind a proxy is the other
mistake: every request then arrives from the proxy, so one person getting a
password wrong slows down everybody.

### What a refusal says

Every refusal of a password reads the same and costs the same. An unknown
username, a wrong password and an account somebody has just guessed at a
hundred times are one answer. Each one charges a full argon2id pass. The wait
is charged whether or not the account exists, so neither the answer nor the
time it takes says which accounts are on this server.

The `429` for a full queue is the one other answer. No password is checked for
it, so it charges no argon2id pass. It depends only on the address, so it says
nothing about the account either.

## Open items

- one login count shared by every process of a deployment with shared storage.
  Each process counts on its own. The count should follow the storage adapter,
  as [Guessing](#guessing) describes.
