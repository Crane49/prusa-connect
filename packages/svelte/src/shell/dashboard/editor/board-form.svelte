<!--
  The fields of the dashboard dialog: the name, and for a new dashboard the
  template it starts from.

  It is made new each time the dialog opens on an act, so its fields start
  from that act. A new dashboard is written at once and opened in the editor,
  ready to move things. A new name goes into the open draft like any other
  change, so it is saved with the rest and taken back with the rest.
-->
<script lang="ts">
  import { Button, DialogFooter, Input, Label } from '../../../primitives';
  import { untrack } from 'svelte';

  import {
    type DashboardTemplate,
    draftFromTemplate,
    MAX_DASHBOARD_NAME_BYTES,
    validDashboardName,
  } from '../../../domain';
  import { t } from '../../../i18n';
  import { dashboardContext } from '../context';
  import FaultNote from '../fault-note.svelte';
  import type { DashboardAct } from './acts';
  import TemplateChoice from './template-choice.svelte';

  let { act, onClose }: { act: DashboardAct; onClose: () => void } = $props();

  const { dashboards, editor, navigation, cameras, printers } =
    dashboardContext();
  const uid = $props.id();

  const start = untrack(() => act);
  let name = $state(start.kind === 'rename' ? start.name : '');
  // One printer is best served by the dashboard of one printer.
  let template = $state<DashboardTemplate>(
    untrack(() => printers.views.length) === 1 ? 'printer' : 'farm',
  );
  let refused = $state<unknown>(undefined);

  const nameOk = $derived(validDashboardName(name));
  const ready = $derived(nameOk);

  const submit = async (event: SubmitEvent): Promise<void> => {
    event.preventDefault();
    if (!ready || dashboards.busy) return;

    if (start.kind === 'rename') {
      editor.rename(name.trim());
      onClose();

      return;
    }

    const first = printers.views[0]?.id;
    const made = await dashboards.create(
      draftFromTemplate(
        template,
        name,
        first,
        first === undefined
          ? cameras.items[0]?.name
          : (cameras.of(first) ?? cameras.items[0])?.name,
      ),
    );
    if (made === undefined) {
      refused = dashboards.writeFault;
      dashboards.dismiss();

      return;
    }

    onClose();
    // The new address draws a new screen, which starts the editor itself.
    navigation.open(made.id, { edit: true });
  };
</script>

<form class="grid gap-y-4" onsubmit={(event) => void submit(event)}>
  <FaultNote fault={refused} />
  <div class="grid gap-y-2">
    <Label for="{uid}-name">{t('dashboard.dialog.name')}</Label>
    <Input
      id="{uid}-name"
      name="name"
      required
      autocomplete="off"
      aria-invalid={name !== '' && !nameOk}
      aria-describedby="{uid}-name-note"
      value={name}
      oninput={(event: Event & { currentTarget: HTMLInputElement }) => {
        name = event.currentTarget.value;
      }}
    />
    <p id="{uid}-name-note" class="text-xs text-muted-foreground">
      {t('dashboard.dialog.nameNote', { most: MAX_DASHBOARD_NAME_BYTES })}
    </p>
  </div>
  {#if start.kind === 'create'}
    <TemplateChoice
      value={template}
      onchange={(next: DashboardTemplate) => {
        template = next;
      }}
    />
  {/if}
  <DialogFooter>
    <Button type="button" variant="secondary" onclick={onClose}>
      {t('dashboard.action.cancel')}
    </Button>
    <Button type="submit" disabled={!ready || dashboards.busy}>
      {start.kind === 'rename'
        ? t('dashboard.action.apply')
        : t('dashboard.action.create')}
    </Button>
  </DialogFooter>
</form>
