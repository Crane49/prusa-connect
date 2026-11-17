<!--
  What a person reads about a release before installing it, beside what it
  asks for: the licence it is published under and every connection it makes
  outside Prusa Connect, with why and what it sends. A release that says
  nothing about its connections is shown as such, never as one that connects
  nowhere.
-->
<script lang="ts">
  import type { components } from '@prusa/connect';
  import { translateFor } from '@prusa/connect-ui/i18n';

  import { APP_NAMESPACE } from '../../i18n';

  type ModuleEgress = components['schemas']['ModuleEgress'];

  let {
    license,
    egress,
    egressKnown,
  }: {
    /** The licence the release names, and undefined when it names none. */
    license: string | undefined;
    /** The release's connections outside Prusa Connect. */
    egress: readonly ModuleEgress[];
    /** Whether the release declares them; false is a release that says nothing. */
    egressKnown: boolean;
  } = $props();

  const t = translateFor(APP_NAMESPACE);

  const lineOf = (one: ModuleEgress): string => {
    const ports = (one.ports ?? []).join(', ');
    const browser = one.from === 'ui';

    if (one.option !== undefined && one.option !== '') {
      return t(
        browser
          ? 'app.settings.extensions.egress.browserOption'
          : 'app.settings.extensions.egress.serverOption',
        { option: one.option, ports },
      );
    }

    return t(
      browser
        ? 'app.settings.extensions.egress.browserHost'
        : 'app.settings.extensions.egress.serverHost',
      { host: one.host ?? '', ports },
    );
  };
</script>

<div class="flex flex-col gap-2 text-sm" data-part="disclosure">
  <p data-part="licence">
    {license !== undefined && license !== ''
      ? t('app.settings.extensions.licence.named', { licence: license })
      : t('app.settings.extensions.licence.none')}
  </p>

  {#if !egressKnown}
    <p data-part="egress-unknown">
      {t('app.settings.extensions.egress.unknown')}
    </p>
  {:else if egress.length === 0}
    <p data-part="egress-none">{t('app.settings.extensions.egress.none')}</p>
  {:else}
    <div class="flex flex-col gap-1" data-part="egress">
      <p>{t('app.settings.extensions.egress.label')}</p>
      <ul class="list-disc ps-5">
        {#each egress as one, index (index)}
          <li>
            <span class="font-medium">{lineOf(one)}</span>
            <span class="text-muted-foreground">
              {t('app.settings.extensions.egress.why', {
                why: one.why,
                sends: one.sends,
              })}
            </span>
          </li>
        {/each}
      </ul>
    </div>
  {/if}
</div>
