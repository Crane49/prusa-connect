<!--
  A page an installed module adds, at `/m/<namespace>/<path>`.

  The screen holds no markup of the module's own. It names the address and the
  library draws whatever the bundle registered for it, inside a boundary, so a
  module that throws costs its own screen and not the app.

  The empty answers are different facts and read differently. Before the
  loader has run, the bundle is on its way. After it has run, this server has
  no such page: the address was mistyped, or the module that served it was
  removed. A module the operator's key keeps off is the last case, which the
  listing marks with the reason ModulesDisabled (ADR-20261019-h76 rule 11). Only a
  module whose own flag is on is promised to open again with the key on.

  The screen reads the printers, as every other screen that shows them does.
  A module reads them through its host and cannot ask for a read itself. A page
  opened straight from its address would otherwise see none.
-->
<script lang="ts">
  import { translateFor } from '@prusa/connect-ui/i18n';
  import { NoLongerHere } from '@prusa/connect-ui/primitives';
  import { getSlotContext, ModulePage } from '@prusa/connect-ui/slots';
  import { onMount } from 'svelte';

  import type { AppContext } from '../screens';
  import { APP_NAMESPACE } from '../i18n';
  import { PATHS } from '../routes';

  const t = translateFor(APP_NAMESPACE);

  let { ctx }: { ctx: AppContext } = $props();

  const namespace = $derived(ctx.params.namespace ?? '');
  const path = $derived(ctx.params.path ?? '');

  const context = getSlotContext();
  const ready = $derived(context?.loader?.ready ?? false);
  const held = $derived(
    ctx.modules.items.some(
      (instance) =>
        instance.namespace === namespace &&
        instance.enabled &&
        (instance.status.conditions ?? []).some(
          (condition) =>
            condition.reason === 'ModulesDisabled' ||
            condition.reason === 'SafeMode',
        ),
    ),
  );

  onMount(() => {
    void ctx.fleet.load();
  });
</script>

<ModulePage {namespace} {path} {fallback} />

{#snippet fallback()}
  {#if ready && !held}
    <NoLongerHere
      headingLevel="h1"
      heading={t('app.module.absent')}
      linkLabel={t('app.settings.extensions.missingList')}
      href={PATHS.settingsExtensions}
    />
  {:else}
    <p class="text-sm text-muted-foreground">
      {#if !ready}
        {t('app.module.opening')}
      {:else}
        {t(
          ctx.settings.safeMode === undefined
            ? 'app.module.modulesOff'
            : 'app.module.safeMode',
        )}
      {/if}
    </p>
  {/if}
{/snippet}
