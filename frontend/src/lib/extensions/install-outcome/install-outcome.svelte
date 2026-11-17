<!--
  What the server answered to an approved install or update, kept on the
  page until the person closes it.

  An install can succeed and still not be what the person expects: a
  permission the organization could not grant was left out, the module is
  not running yet, or the server has to restart first. Each of those is its
  own line, so "installed" never stands alone above a module that does not
  work. An update also lists what the new release asked for that the old one
  did not.
-->
<script lang="ts">
  import XIcon from '@lucide/svelte/icons/x';
  import { translateFor } from '@prusa/connect-ui/i18n';
  import {
    Alert,
    AlertDescription,
    AlertTitle,
    Button,
  } from '@prusa/connect-ui/primitives';

  import { APP_NAMESPACE } from '../../i18n';
  import type { ModuleCatalogue } from '../../stores/module-catalogue.svelte';
  import { RequestList } from '../request-list';
  import { listLines } from '../requests';

  let {
    catalogue,
    name,
  }: {
    catalogue: ModuleCatalogue;
    /** The name the listing gives the module. */
    name: string;
  } = $props();

  const t = translateFor(APP_NAMESPACE);

  const outcome = $derived(catalogue.outcome);
  const answer = $derived(outcome?.answer);
  const withheld = $derived(listLines(answer?.withheld, answer?.namespace));
  const added = $derived(listLines(answer?.added, answer?.namespace));
  const idle = $derived(
    answer !== undefined && !answer.restart && !answer.running,
  );
  const troubled = $derived(
    answer !== undefined &&
      (withheld.length > 0 || answer.restart || !answer.running),
  );
</script>

{#if outcome !== undefined && answer !== undefined}
  <Alert
    variant={troubled ? 'warning' : 'success'}
    data-part="install-outcome"
    aria-live="polite"
  >
    <AlertTitle class="flex items-start justify-between gap-2">
      <span>
        {outcome.update
          ? t('app.settings.extensions.outcome.updated', {
              name,
              version: answer.version,
            })
          : t('app.settings.extensions.outcome.installed', {
              name,
              version: answer.version,
            })}
      </span>
      <Button
        variant="ghost"
        size="icon-sm"
        label={t('app.settings.extensions.outcome.dismiss')}
        onclick={() => {
          catalogue.dismissOutcome();
        }}
      >
        <XIcon aria-hidden="true" />
      </Button>
    </AlertTitle>
    <AlertDescription class="flex flex-col gap-2">
      {#if withheld.length > 0}
        <div data-part="withheld">
          <RequestList
            part="withheld-list"
            instance={outcome.offer.name}
            requests={withheld}
            label={t('app.settings.extensions.outcome.withheld')}
          />
          <p>{t('app.settings.extensions.outcome.withheld.how')}</p>
        </div>
      {/if}
      {#if outcome.update && added.length > 0}
        <RequestList
          part="added"
          instance={outcome.offer.name}
          requests={added}
          label={t('app.settings.extensions.outcome.added')}
        />
      {/if}
      {#if answer.restart}
        <p data-part="restart">
          {t('app.settings.extensions.outcome.restart')}
        </p>
      {:else if !answer.enabled}
        <p data-part="switched-off">
          {t('app.settings.extensions.outcome.off')}
        </p>
      {:else if idle}
        <p data-part="not-running">
          {t('app.settings.extensions.outcome.notRunning')}
        </p>
      {/if}
    </AlertDescription>
  </Alert>
{/if}
