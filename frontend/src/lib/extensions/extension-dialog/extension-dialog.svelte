<!--
  The one question the Extensions page asks before it acts: install, update,
  remove, and the approval of an install that waits for one.

  An install says the module is native code with no sandbox, and lists every
  request the person grants by confirming: each permission, each event it
  publishes, whether it may ask about people it is not serving, and the room
  for its records. The same list goes to the server as `expect`, so a release
  that asks for more is refused instead of granted. A network port is
  reachable from other devices, so it takes a tick of its own, unticked
  until the person chooses it. A release that asks for a database of its own
  lets the person pick where it goes first.

  A release that requires extensions this server does not have names them
  first, each with its release and its requests: the install adds them, and
  the one approval covers them all (ADR-20261027-e6k). A removal names the
  extensions an install added for this one that nothing else needs, and
  removes them too only when the person ticks it.

  An install or an update says how far the marketplace vouches for what it
  adds, the extensions the plan adds included. An official extension needs no
  word. A trusted one is told to have had its code reviewed, which is no
  guarantee. An unverified one passed only automatic checks: the dialog warns,
  and the install waits until the person ticks that they accept the risk and
  trust who publishes it.

  A row that does not know its release's requests cannot list them. The
  dialog then says the server installs the release switched off to read them,
  and once it has, the approval opens on its own with what the server read.
  A release that asked for more than was shown opens it too, with the
  difference first.

  It is drawn once for the whole page, so a row's button and a link from the
  marketplace open the same question.
-->
<script lang="ts">
  import ShieldAlertIcon from '@lucide/svelte/icons/shield-alert';
  import ShieldCheckIcon from '@lucide/svelte/icons/shield-check';
  import { untrack } from 'svelte';
  import { formatList, translateFor } from '@prusa/connect-ui/i18n';
  import {
    Alert,
    AlertDescription,
    AlertTitle,
    Badge,
    Checkbox,
    ConfirmDialog,
    Input,
    Label,
  } from '@prusa/connect-ui/primitives';

  import { APP_NAMESPACE } from '../../i18n';
  import type {
    ModuleCatalogue,
    PendingApproval,
  } from '../../stores/module-catalogue.svelte';
  import { shortNameOf } from '../dependencies';
  import { ExtensionDatabase } from '../extension-database';
  import { ExtensionPlan } from '../extension-plan';
  import { memoryEstimate, type BudgetSource, offerBudget } from '../memory';
  import { MemoryEstimate } from '../memory-estimate';
  import { ExtensionDisclosure } from '../extension-disclosure';
  import { RequestList } from '../request-list';
  import {
    asksForDatabase,
    asksForPorts,
    databaseChoiceOf,
    type DatabaseMode,
    type ModuleListen,
    movedPorts,
    portOfText,
    portsOf,
    listLines,
    listRequests,
    type ModuleRequest,
    requestsOfOffer,
  } from '../requests';
  import type { ExtensionRow } from '../rows';
  import { needsConsent, trustNoticeOf } from '../trust';
  import type { Asking } from './types';

  let {
    catalogue,
    asking,
    rows,
    onclose,
  }: {
    catalogue: ModuleCatalogue;
    /** What the person asked for, and undefined when nothing is asked. */
    asking: Asking | undefined;
    /** Every row of the listing, so a pending install is named as its row is. */
    rows: readonly ExtensionRow[];
    onclose: () => void;
  } = $props();

  const t = translateFor(APP_NAMESPACE);

  type Mode =
    | { kind: 'remove'; row: ExtensionRow }
    | { kind: 'enable'; row: ExtensionRow }
    | {
        kind: 'confirm' | 'read';
        row: ExtensionRow;
        update: boolean;
        version: string;
        name: string;
        requests: readonly ModuleRequest[];
        ports: readonly ModuleListen[];
      }
    | { kind: 'approve' | 'changed'; pending: PendingApproval; name: string };

  // A pending approval comes first: the server is waiting on it.
  const mode = $derived.by((): Mode | undefined => {
    const pending = catalogue.pending;
    if (pending !== undefined) {
      if (catalogue.acting !== undefined) return undefined;
      const row = rows.find((one) => one.module === pending.offer.module);
      const name = row?.title ?? pending.offer.title ?? pending.offer.name;

      return {
        kind: pending.cause === 'unread' ? 'approve' : 'changed',
        pending,
        name,
      };
    }

    if (asking === undefined) return undefined;
    const { row, act } = asking;
    if (act === 'remove' || act === 'enable') return { kind: act, row };
    if (row.offer === undefined) return undefined;

    const version = asking.version ?? row.version;
    const known =
      version === row.version ? requestsOfOffer(row.offer) : undefined;

    return {
      kind: known === undefined ? 'read' : 'confirm',
      row,
      update: act === 'update',
      version,
      name: row.offer.name,
      requests: known === undefined ? [] : listRequests(known),
      ports: portsOf(known),
    };
  });

  // The release the question is about, for its licence and its connections
  // outside Prusa Connect. A removal or a switch shows neither.
  const disclosed = $derived.by(() => {
    if (mode === undefined) return undefined;
    if ('pending' in mode) return mode.pending.offer;
    if (mode.kind === 'confirm' || mode.kind === 'read') return mode.row.offer;

    return undefined;
  });

  // The plan of an install or an update, read once the question opens, and
  // what a removal meets, read every time one opens.
  const planned = $derived.by(() => {
    if (
      mode === undefined ||
      (mode.kind !== 'confirm' && mode.kind !== 'read')
    ) {
      return undefined;
    }
    const offer = mode.row.offer;

    return offer === undefined ? undefined : { offer, version: mode.version };
  });
  const plan = $derived(
    planned === undefined
      ? undefined
      : catalogue.planOf(planned.offer, planned.version),
  );
  $effect(() => {
    const asked = planned;
    if (asked !== undefined && plan === undefined) {
      untrack(() => void catalogue.readPlan(asked.offer, asked.version));
    }
  });

  // The pool is read whenever a question that enables a module opens.
  const memoryAsked = $derived(mode !== undefined && mode.kind !== 'remove');
  $effect(() => {
    if (memoryAsked) untrack(() => void catalogue.readMemory());
  });
  // The budget the question is about. A module the server already holds a
  // budget for shows it when the operator set it, and a release that brings a
  // new recommendation shows that; a release not installed shows the author's
  // recommendation, and a pending one the requests the server read. `held` is
  // what the module already counts in the pool: an enabled module's budget.
  // A module that runs no process has no budget: an installed one carries no
  // memory status, and a release on offer says so in `process`.
  const own = $derived.by(
    ():
      | { budgetBytes: number; source: BudgetSource; heldBytes: number }
      | undefined => {
      if (
        mode === undefined ||
        mode.kind === 'remove' ||
        mode.kind === 'read'
      ) {
        return undefined;
      }
      if ('pending' in mode) {
        if (!mode.pending.offer.process) return undefined;

        return {
          ...offerBudget(mode.pending.requests.memory_mib),
          heldBytes: 0,
        };
      }
      const memory = mode.row.instance?.status.memory;
      const runs =
        mode.kind === 'enable'
          ? memory !== undefined
          : mode.row.offer?.process !== false;
      if (!runs) return undefined;
      const heldBytes =
        memory !== undefined && mode.row.instance?.enabled === true
          ? memory.budget_bytes
          : 0;
      if (
        memory !== undefined &&
        (mode.kind === 'enable' || memory.budget_source === 'operator')
      ) {
        return {
          budgetBytes: memory.budget_bytes,
          source: memory.budget_source,
          heldBytes,
        };
      }

      return { ...offerBudget(mode.row.offer?.memory_mib), heldBytes };
    },
  );
  const estimate = $derived(
    own === undefined
      ? undefined
      : memoryEstimate({ ...own, pool: catalogue.memoryPool }),
  );
  // The pool as the modules the plan adds meet it: with this module's budget
  // already in it.
  const planPool = $derived.by(() => {
    const pool = catalogue.memoryPool;
    if (pool === undefined || own === undefined) return pool;

    return {
      ...pool,
      committed_bytes: pool.committed_bytes - own.heldBytes + own.budgetBytes,
    };
  });

  const removedNamespace = $derived(
    mode?.kind === 'remove' ? mode.row.namespace : undefined,
  );
  $effect(() => {
    const asked = removedNamespace;
    if (asked !== undefined) {
      untrack(() => void catalogue.readRemoval(asked));
    }
  });
  const removal = $derived(catalogue.removalOf(removedNamespace));
  const unused = $derived(
    removal?.state === 'ready' ? (removal.removal.unused ?? []) : [],
  );
  let withUnused = $state(false);

  // Whether the release asks for a port, on whichever question is open.
  const askedPorts = $derived.by((): readonly ModuleListen[] => {
    if (
      mode === undefined ||
      mode.kind === 'remove' ||
      mode.kind === 'enable'
    ) {
      return [];
    }

    return 'pending' in mode ? portsOf(mode.pending.requests) : mode.ports;
  });
  // The ports the modules the plan adds ask for. One approval covers them
  // with the module's own, so they are listed beside them (ADR-20261027-e6k
  // rule 16). They are granted at the numbers they ask for.
  const planPorts = $derived(
    plan?.state === 'ready' && mode?.kind === 'confirm'
      ? plan.dependencies.flatMap((one) =>
          (one.listen ?? []).map((port) => ({
            module: one.title ?? one.name,
            port,
          })),
        )
      : [],
  );
  const ports = $derived(
    asksForPorts({ listen: [...askedPorts] }) || planPorts.length > 0,
  );
  // An install or update acts on the plan the person read, so it waits until
  // the plan is read and fits. A removal waits while the check of what it
  // leaves unused is read; a check that failed offers nothing to add, and the
  // server judges the removal again.
  const waiting = $derived(
    (planned !== undefined && plan?.state !== 'ready') ||
      (mode?.kind === 'remove' && (removal?.state ?? 'loading') === 'loading'),
  );
  let allowPorts = $state(false);
  // What the person typed for each port, by name. A port not typed keeps
  // the number the manifest asks for.
  let typedPorts = $state<Record<string, string>>({});
  const portsId = $props.id();

  // Where the module's own database goes, for a release that asks for one
  // (ADR-20261021-bqc rule 5). An update keeps the database it has unless the person
  // picks another place.
  const askedDatabase = $derived.by((): boolean => {
    if (
      mode === undefined ||
      mode.kind === 'remove' ||
      mode.kind === 'enable'
    ) {
      return false;
    }

    return 'pending' in mode
      ? asksForDatabase(mode.pending.requests)
      : mode.requests.some((request) => request.kind === 'database');
  });
  const databaseRow = $derived.by((): ExtensionRow | undefined => {
    if (
      mode === undefined ||
      mode.kind === 'remove' ||
      mode.kind === 'enable'
    ) {
      return undefined;
    }
    if (!('pending' in mode)) return mode.row;
    const module = mode.pending.offer.module;

    return rows.find((one) => one.module === module);
  });
  const databaseUpdate = $derived(databaseRow?.instance !== undefined);
  let pickedDatabase = $state<DatabaseMode | undefined>(undefined);
  let typedDSN = $state('');
  const databaseMode = $derived<DatabaseMode>(
    pickedDatabase ?? (databaseUpdate ? 'keep' : 'sqlite'),
  );

  const typedFor = (port: ModuleListen): string =>
    typedPorts[port.name] ?? String(port.port);

  const beta = $derived(
    mode !== undefined &&
      ('row' in mode
        ? mode.row.channel === 'beta'
        : mode.pending.offer.channel === 'beta'),
  );

  // How far the marketplace vouches for what an install or an update adds:
  // the release picked, then each extension the plan adds with it.
  const trust = $derived.by(() => {
    if (
      mode === undefined ||
      (mode.kind !== 'confirm' && mode.kind !== 'read')
    ) {
      return undefined;
    }
    const added = plan?.state === 'ready' ? plan.dependencies : [];

    return trustNoticeOf([
      { title: mode.row.title, author: mode.row.author, trust: mode.row.trust },
      ...added.map((one) => ({
        title: one.title ?? one.name,
        author: one.author,
        trust: one.trust,
      })),
    ]);
  });
  const consentAsked = $derived(trust !== undefined && needsConsent(trust));
  let accepted = $state(false);
  const consentId = `${portsId}-consent`;

  const removeNote = (): string => {
    if (catalogue.modulesOn) {
      return t('app.settings.extensions.confirm.remove.note');
    }

    return catalogue.safeMode
      ? t('app.settings.extensions.confirm.remove.noteSafeMode')
      : t('app.settings.extensions.confirm.remove.noteModulesOff');
  };

  // What the dialog says, by what is being asked. One table rather than a
  // chain of conditions, so each act's three texts are read together.
  const text = $derived.by(() => {
    switch (mode?.kind) {
      case undefined:
        return { title: '', note: '', label: '' };
      case 'remove':
        return {
          title: t('app.settings.extensions.confirm.remove.title', {
            name: mode.row.title,
          }),
          // The usual note promises a reinstall the server would refuse.
          note: removeNote(),
          label: t('app.settings.extensions.offer.remove'),
        };
      case 'enable':
        return {
          title: t('app.settings.extensions.confirm.enable.title', {
            name: mode.row.title,
          }),
          note: t('app.settings.extensions.confirm.enable.note'),
          label: t('app.settings.extensions.confirm.enable.label'),
        };
      case 'confirm':
      case 'read': {
        const note = t(
          mode.row.trust === 'official'
            ? 'app.settings.extensions.confirm.install.noteOfficial'
            : 'app.settings.extensions.confirm.install.note',
          { name: mode.row.title, version: mode.version },
        );
        const title = mode.update
          ? t('app.settings.extensions.confirm.update.title', {
              name: mode.row.title,
              version: mode.version,
            })
          : t('app.settings.extensions.confirm.install.title', {
              name: mode.row.title,
            });
        if (mode.kind === 'read') {
          return {
            title,
            note,
            label: t('app.settings.extensions.confirm.read.label'),
          };
        }
        const label = mode.update
          ? t('app.settings.extensions.offer.update', {
              version: mode.version,
            })
          : t('app.settings.extensions.offer.install');

        return { title, note, label };
      }
      case 'approve':
        return {
          title: t('app.settings.extensions.confirm.approve.title', {
            name: mode.name,
          }),
          note: t('app.settings.extensions.confirm.approve.note', {
            name: mode.name,
            version: mode.pending.version,
          }),
          label: t('app.settings.extensions.confirm.approve.label'),
        };
      case 'changed':
        return {
          title: t('app.settings.extensions.confirm.changed.title', {
            name: mode.name,
          }),
          note: t('app.settings.extensions.confirm.changed.note'),
          label: t('app.settings.extensions.confirm.changed.label'),
        };
    }
  });

  // The version a link named, when it is not the one the row offers.
  const otherVersion = $derived(
    mode !== undefined &&
      'row' in mode &&
      mode.kind !== 'remove' &&
      mode.kind !== 'enable' &&
      mode.version !== mode.row.version
      ? { version: mode.version, latest: mode.row.version }
      : undefined,
  );

  const close = (): void => {
    if (mode?.kind === 'approve' || mode?.kind === 'changed') {
      catalogue.dismissPending();
    }
    allowPorts = false;
    typedPorts = {};
    pickedDatabase = undefined;
    typedDSN = '';
    withUnused = false;
    accepted = false;
    onclose();
  };

  const confirm = (): void => {
    const now = mode;
    const approveListen = ports && allowPorts;
    const listen = approveListen ? movedPorts(askedPorts, typedPorts) : {};
    const database = askedDatabase
      ? databaseChoiceOf(databaseMode, typedDSN)
      : undefined;
    const alsoUnused = withUnused && unused.length > 0;
    withUnused = false;
    allowPorts = false;
    accepted = false;
    typedPorts = {};
    pickedDatabase = undefined;
    typedDSN = '';
    onclose();

    switch (now?.kind) {
      case 'remove':
        void catalogue.remove(now.row, alsoUnused);
        break;
      case 'enable':
        if (now.row.instance !== undefined) {
          void catalogue.writeInstance(now.row.instance, { enabled: true });
        }
        break;
      case 'confirm':
      case 'read':
        if (now.row.offer !== undefined) {
          void catalogue.install(now.row.offer, {
            version: now.version,
            approveListen,
            listen,
            ...(database === undefined ? {} : { database }),
          });
        }
        break;
      case 'approve':
      case 'changed':
        void catalogue.approvePending(approveListen, listen, database);
        break;
      case undefined:
        break;
    }
  };
</script>

<ConfirmDialog
  open={mode !== undefined}
  onOpenChange={(open: boolean) => {
    if (!open) close();
  }}
  title={text.title}
  description={text.note}
  confirmLabel={text.label}
  onConfirm={confirm}
  confirmDisabled={waiting || (consentAsked && !accepted)}
  variant={mode?.kind === 'remove' ? 'destructive' : 'confirm'}
>
  {#if mode?.kind === 'remove' && mode.row.instance?.module?.asked?.data === true}
    <p class="text-sm" data-part="remove-data">
      {t('app.settings.extensions.confirm.remove.data', {
        instance: mode.row.instance.name,
      })}
    </p>
  {/if}
  {#if mode?.kind === 'remove' && mode.row.instance?.module?.asked?.database === true}
    <p class="text-sm" data-part="remove-database">
      {t('app.settings.extensions.confirm.remove.database')}
    </p>
  {/if}
  {#if mode?.kind === 'remove' && unused.length > 0}
    <div class="flex items-start gap-2 text-sm" data-part="unused">
      <Checkbox
        id={`${portsId}-unused`}
        bind:checked={withUnused}
        class="mt-0.5"
      />
      <div class="flex flex-col gap-0.5">
        <Label for={`${portsId}-unused`}>
          {t('app.settings.extensions.confirm.remove.unused.label', {
            count: unused.length,
          })}
        </Label>
        <ul class="text-xs text-muted-foreground">
          {#each unused as one (one.instance)}
            <li>
              {one.title ?? shortNameOf(one.module)}
              <code>{one.version ?? ''}</code>
            </li>
          {/each}
        </ul>
      </div>
    </div>
  {/if}
  {#if mode !== undefined && mode.kind !== 'remove'}
    <div class="flex flex-col gap-3 text-sm" data-part="dialog-body">
      {#if beta}
        <p class="flex flex-wrap items-center gap-2" data-part="beta-note">
          <Badge variant="warning">{t('app.settings.extensions.beta')}</Badge>
          {t('app.settings.extensions.confirm.beta')}
        </p>
      {/if}

      {#if trust !== undefined && trust.unverified.length > 0}
        <Alert variant="destructive" data-part="trust-community">
          <ShieldAlertIcon aria-hidden="true" />
          <AlertTitle>
            {t('app.settings.extensions.confirm.trust.community.title', {
              count: trust.unverified.length,
            })}
          </AlertTitle>
          <AlertDescription>
            {t('app.settings.extensions.confirm.trust.community.note', {
              names: formatList(trust.unverified, 'conjunction'),
              count: trust.unverified.length,
            })}
          </AlertDescription>
        </Alert>
        <div class="flex items-start gap-2" data-part="trust-consent">
          <Checkbox id={consentId} bind:checked={accepted} class="mt-0.5" />
          <Label for={consentId}>
            {trust.authors.length > 0
              ? t('app.settings.extensions.confirm.trust.community.accept', {
                  authors: formatList(trust.authors, 'conjunction'),
                })
              : t('app.settings.extensions.confirm.trust.community.acceptAny', {
                  count: trust.unverified.length,
                })}
          </Label>
        </div>
      {:else if trust !== undefined && trust.trusted.length > 0}
        <Alert variant="info" data-part="trust-verified">
          <ShieldCheckIcon aria-hidden="true" />
          <AlertTitle>
            {t('app.settings.extensions.confirm.trust.verified.title', {
              count: trust.trusted.length,
            })}
          </AlertTitle>
          <AlertDescription>
            {t('app.settings.extensions.confirm.trust.verified.note', {
              names: formatList(trust.trusted, 'conjunction'),
              count: trust.trusted.length,
            })}
          </AlertDescription>
        </Alert>
      {/if}

      {#if disclosed !== undefined}
        <ExtensionDisclosure
          license={disclosed.license}
          egress={disclosed.egress ?? []}
          egressKnown={disclosed.egress_known ?? false}
        />
      {/if}

      {#if otherVersion !== undefined}
        <p data-part="other-version">
          {t('app.settings.extensions.confirm.version.other', otherVersion)}
        </p>
      {/if}

      {#if mode.kind === 'confirm'}
        <RequestList
          instance={mode.name}
          requests={mode.requests}
          label={t('app.settings.extensions.confirm.asks')}
        />
        {#if mode.update}
          <p data-part="kept">
            {t('app.settings.extensions.confirm.update.kept')}
          </p>
        {/if}
      {:else if mode.kind === 'read'}
        <p data-part="read-first">
          {t('app.settings.extensions.confirm.read.note')}
        </p>
        {#if mode.update}
          <p data-part="read-stops">
            {t('app.settings.extensions.confirm.read.update')}
          </p>
        {/if}
      {:else if mode.kind === 'approve'}
        <RequestList
          instance={mode.pending.offer.name}
          requests={listRequests(mode.pending.requests)}
          label={t('app.settings.extensions.confirm.asks')}
        />
        <p>{t('app.settings.extensions.confirm.approve.cancel')}</p>
      {:else if mode.kind === 'changed'}
        <RequestList
          part="requests-found"
          instance={mode.pending.offer.name}
          requests={listLines(mode.pending.found, mode.pending.namespace)}
          label={t('app.settings.extensions.confirm.changed.found')}
        />
        <RequestList
          instance={mode.pending.offer.name}
          requests={listRequests(mode.pending.requests)}
          label={t('app.settings.extensions.confirm.asks')}
        />
      {/if}

      {#if estimate !== undefined}
        <MemoryEstimate {estimate} />
      {/if}

      <!-- What the install also adds comes after the extension's own
           requests, so the person reads what they picked first. -->
      {#if mode.kind === 'confirm' || mode.kind === 'read'}
        <ExtensionPlan {plan} pool={planPool} />
      {/if}

      {#if askedDatabase && mode.kind !== 'read'}
        <ExtensionDatabase
          mode={databaseMode}
          onpick={(next: DatabaseMode) => {
            pickedDatabase = next;
          }}
          bind:dsn={typedDSN}
          update={databaseUpdate}
          current={databaseRow?.instance?.module?.database}
        />
      {/if}

      {#if ports}
        <div class="flex items-start gap-2" data-part="ports">
          <Checkbox id={portsId} bind:checked={allowPorts} class="mt-0.5" />
          <div class="flex flex-col gap-0.5">
            <Label for={portsId}>
              {t('app.settings.extensions.confirm.ports')}
            </Label>
            <span class="text-xs text-muted-foreground">
              {t('app.settings.extensions.confirm.portsHelp')}
            </span>
          </div>
        </div>

        {#if planPorts.length > 0}
          <ul class="flex flex-col gap-1 ps-6 text-xs" data-part="plan-ports">
            {#each planPorts as { module, port } (`${module}-${port.name}`)}
              <li>
                {t('app.settings.extensions.confirm.planPort', {
                  module,
                  port: String(port.port),
                  network: port.network.toUpperCase(),
                  why: port.why,
                })}
              </li>
            {/each}
          </ul>
        {/if}

        {#if allowPorts && askedPorts.length > 0}
          <div class="flex flex-col gap-2 ps-6" data-part="port-numbers">
            {#each askedPorts as port (port.name)}
              {@const inputId = `${portsId}-${port.name}`}
              {@const valid = portOfText(typedFor(port)) !== undefined}
              <div class="flex flex-col gap-1">
                <Label for={inputId} variant="hint">
                  {t('app.settings.extensions.confirm.portNumber', {
                    network: port.network.toUpperCase(),
                    why: port.why,
                  })}
                </Label>
                <Input
                  id={inputId}
                  type="number"
                  inputmode="numeric"
                  min="1"
                  max="65535"
                  size="sm"
                  class="w-32"
                  value={typedFor(port)}
                  aria-invalid={!valid}
                  oninput={(event: Event) => {
                    typedPorts = {
                      ...typedPorts,
                      [port.name]: (event.currentTarget as HTMLInputElement)
                        .value,
                    };
                  }}
                />
                {#if !valid}
                  <span class="text-xs text-destructive" role="status">
                    {t('app.settings.extensions.confirm.portInvalid', {
                      port: String(port.port),
                    })}
                  </span>
                {/if}
              </div>
            {/each}
            <span class="text-xs text-muted-foreground">
              {t('app.settings.extensions.confirm.portsMove')}
            </span>
          </div>
        {/if}
      {/if}
    </div>
  {/if}
</ConfirmDialog>
