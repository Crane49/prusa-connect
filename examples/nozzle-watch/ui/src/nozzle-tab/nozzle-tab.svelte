<script module lang="ts">
  /** The namespace the spools module is installed under. */
  const SPOOLS = 'com.github.aux.acme.spools';
</script>

<script lang="ts">
  import { formatDate } from '@prusa/connect-ui/i18n';
  import { ModuleComponent } from '@prusa/connect-ui/slots';

  import { heldHost } from '../host';
  import { changedAt } from '../mark-changed';

  interface Props {
    /** The printer this tab is about. It is all a device-tab is handed (ADR-20260911-4p6 rule 10). */
    deviceId: string;
  }

  let { deviceId }: Props = $props();

  // The tab strip already draws the manifest's title, so this panel is only
  // the body: what `mark-changed` last wrote for this printer, read straight
  // off the store the two share.
  const host = heldHost();
  const t = host.t;

  const marked = $derived(changedAt(host, deviceId));

  // The spools module's picker, drawn here when that module is installed
  // (ADR-20261027-wvj rules 20 to 24). This module does not know how spools
  // are kept: it names the component under ui.imports, passes a callback, and
  // reads back the spool a person picked.
  interface Picked {
    id: string;
    material: string;
  }
  interface PickerProps extends Record<string, unknown> {
    onselect: (spool: Picked) => void;
    selected?: string | undefined;
  }

  let loaded = $state<Picked | undefined>(undefined);

  const picker = () =>
    host.components.load<PickerProps>(SPOOLS, 'spool-picker');
</script>

<div class="flex flex-col gap-2">
  <h2 class="text-sm font-semibold">{t('nozzle.tab.heading')}</h2>
  <p class="text-sm text-muted-foreground">
    {marked === undefined ? t('nozzle.tab.never') : formatDate(marked)}
  </p>
  <h2 class="text-sm font-semibold">{t('nozzle.tab.spool')}</h2>
  <ModuleComponent
    load={picker}
    props={{
      onselect: (spool: Picked) => {
        loaded = spool;
      },
      selected: loaded?.id,
    }}
  >
    {#snippet fallback()}
      <p class="text-sm text-muted-foreground">{t('nozzle.tab.noSpools')}</p>
    {/snippet}
  </ModuleComponent>
  {#if loaded}
    <p class="text-sm">
      {t('nozzle.tab.loaded', { spool: loaded.id, material: loaded.material })}
    </p>
  {/if}
</div>
