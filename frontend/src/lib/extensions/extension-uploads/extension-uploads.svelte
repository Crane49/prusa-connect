<!--
  The Uploads tab of a service module: the largest request body each staged
  route takes, which the operator approved at install. A route the module
  names under routes.staged has its body written to the module's data folder,
  and a larger body is refused before the module runs (ADR-20261120-xys rules
  5 and 6). Saving writes the limits with the rest of the instance, which
  restarts the module so it learns the new limit.
-->
<script lang="ts">
  import { formatNumber, translateFor } from '@prusa/connect-ui/i18n';
  import {
    Alert,
    AlertDescription,
    Button,
    FaultAlert,
    Input,
    SettingsPane,
    SettingsRow,
  } from '@prusa/connect-ui/primitives';

  import { APP_NAMESPACE } from '../../i18n';
  import { describeExtensionFault } from '../fault';
  import {
    MAX_STAGED_MIB,
    type ModuleStagedRoute,
    sameRoute,
    stagedOfText,
  } from '../requests';

  let {
    routes,
    asked = [],
    locked,
    saving,
    fault,
    conflict = false,
    onsave,
  }: {
    /** Each staged route the install approved, at the limit it holds now. */
    routes: readonly ModuleStagedRoute[];
    /** What the manifest asked for at install, to say beside each route. */
    asked?: readonly ModuleStagedRoute[];
    /** Why the limits cannot be changed here, already worded. */
    locked?: string | undefined;
    saving: boolean;
    /** What the last write of the limits failed with. */
    fault?: unknown;
    conflict?: boolean;
    /** Writes the limits; resolves true once the server took them. */
    onsave: (routes: ModuleStagedRoute[]) => Promise<boolean>;
  } = $props();

  const t = translateFor(APP_NAMESPACE);
  const id = $props.id();

  const keyOf = (route: ModuleStagedRoute): string =>
    `${route.method} ${route.path}`;

  // What the person typed, by route. A route not typed shows its limit now.
  let typed = $state<Record<string, string>>({});
  const shownFor = (route: ModuleStagedRoute): string =>
    typed[keyOf(route)] ?? String(route.max_mib);

  const chosen = $derived(
    routes.map((route) => ({
      ...route,
      max_mib: stagedOfText(shownFor(route)),
    })),
  );
  const allValid = $derived(
    chosen.every((route) => route.max_mib !== undefined),
  );
  const changed = $derived(
    chosen.some((route, index) => route.max_mib !== routes[index]?.max_mib),
  );

  const askedOf = (route: ModuleStagedRoute): ModuleStagedRoute | undefined =>
    asked.find((one) => sameRoute(one, route));

  const save = async (): Promise<void> => {
    const next = chosen.flatMap((route) =>
      route.max_mib === undefined ? [] : [{ ...route, max_mib: route.max_mib }],
    );
    if (await onsave(next)) typed = {};
  };
</script>

<div class="flex flex-col gap-4" data-part="extension-uploads">
  <SettingsPane
    title={t('app.settings.extensions.uploads.title')}
    description={t('app.settings.extensions.uploads.note', {
      max: formatNumber(MAX_STAGED_MIB),
    })}
  >
    <!-- Padded as the other panes' rows are. A route is one long word, so
         its label takes the row's width and breaks anywhere only where even
         that is too narrow, rather than running over the field. -->
    <div
      class="flex flex-col gap-section-gap p-panel-padding-mobile @lg/pane:p-panel-padding"
    >
      {#each routes as route, index (keyOf(route))}
        {@const rowId = `${id}-${String(index)}`}
        {@const valid = stagedOfText(shownFor(route)) !== undefined}
        {@const wanted = askedOf(route)?.max_mib}
        <!-- A route the module titled reads by its title, with the method
             and path under it for whoever needs them. -->
        {@const title = askedOf(route)?.title}
        <div data-part="upload-route">
          <SettingsRow
            id={rowId}
            layout="switch"
            class="[overflow-wrap:anywhere]"
            label={title ??
              t('app.settings.extensions.uploads.label', {
                route: keyOf(route),
              })}
            description={[
              title === undefined ? undefined : keyOf(route),
              wanted === undefined
                ? undefined
                : t('app.settings.extensions.uploads.asked', {
                    size: formatNumber(wanted),
                  }),
            ]
              .filter((part) => part !== undefined)
              .join(' · ') || undefined}
          >
            {#snippet control()}
              <div class="w-28 shrink-0">
                <Input
                  type="number"
                  inputmode="numeric"
                  min="1"
                  max={MAX_STAGED_MIB}
                  size="sm"
                  class="tabular-nums"
                  value={shownFor(route)}
                  disabled={locked !== undefined || saving}
                  aria-labelledby={`${rowId}-label`}
                  aria-describedby={valid ? undefined : `${rowId}-error`}
                  aria-invalid={!valid}
                  oninput={(event: Event) => {
                    typed = {
                      ...typed,
                      [keyOf(route)]: (event.currentTarget as HTMLInputElement)
                        .value,
                    };
                  }}
                />
              </div>
              <span class="text-sm text-muted-foreground">
                {t('app.settings.extensions.uploads.unit')}
              </span>
            {/snippet}
          </SettingsRow>
          {#if !valid}
            <p
              id={`${rowId}-error`}
              class="pb-3 text-xs text-destructive-text"
              role="status"
            >
              {t('app.settings.extensions.uploads.invalid', {
                max: formatNumber(MAX_STAGED_MIB),
              })}
            </p>
          {/if}
        </div>
      {/each}
    </div>

    {#snippet actions()}
      {#if locked !== undefined}
        <p class="text-sm" data-part="uploads-locked">{locked}</p>
      {:else}
        <Button
          size="sm"
          disabled={saving || !allValid || !changed}
          aria-busy={saving}
          onclick={() => {
            void save();
          }}
        >
          {t('app.settings.extensions.uploads.save')}
        </Button>
      {/if}
    {/snippet}
  </SettingsPane>

  {#if conflict}
    <Alert variant="warning">
      <AlertDescription>
        {t('app.settings.extensions.write.conflict')}
      </AlertDescription>
    </Alert>
  {:else if fault !== undefined}
    <FaultAlert fault={describeExtensionFault(fault, t)} />
  {/if}
</div>
