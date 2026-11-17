<!--
  The fields of the folder dialog: the name, the folder it is in, and a few
  lines about what it holds.

  The refusal the module answers, a name the parent already holds, is drawn
  here, where the person can change the name, and the dialog stays open until
  a write lands.
-->
<script lang="ts">
  import {
    Button,
    DialogFooter,
    Field,
    Input,
    Label,
    Textarea,
  } from '@prusa/connect-ui/primitives';
  import { untrack } from 'svelte';

  import { MAX_FOLDER_DESCRIPTION, MAX_NAME } from '../presentation';
  import { heldSession } from '../session';
  import type { FolderAct } from './folder-act';
  import FaultNote from './fault-note.svelte';
  import FolderSelect from './folder-select.svelte';

  let { act, onClose }: { act: FolderAct; onClose: () => void } = $props();

  const { catalogue, navigation, host } = heldSession();
  const t = host.t;
  const uid = $props.id();

  // The form is made new for each act (folder-dialog.svelte), so the act it
  // was made for is the one it starts from.
  const start = untrack(() => act);
  let name = $state(start.kind === 'change' ? start.folder.name : '');
  let parent = $state(
    start.kind === 'change' ? start.folder.parent : start.parent,
  );
  let description = $state(
    start.kind === 'change' ? (start.folder.description ?? '') : '',
  );
  let refused = $state<unknown>(undefined);

  const slash = $derived(name.includes('/'));
  const ready = $derived(name.trim() !== '' && !slash);

  /** Keeps what the module refused here, and off the page behind the dialog. */
  const keep = (): void => {
    refused = catalogue.fault;
    catalogue.dismiss();
  };

  const submit = async (event: SubmitEvent): Promise<void> => {
    event.preventDefault();
    if (!ready || catalogue.busy) return;

    const draft = {
      name: name.trim(),
      parent,
      description: description.trim(),
    };

    if (start.kind === 'create') {
      const made = await catalogue.addFolder(draft);
      if (made === undefined) {
        keep();

        return;
      }
      onClose();
      navigation.openFolder(made.id);

      return;
    }

    if (await catalogue.changeFolder(start.folder.id, draft)) onClose();
    else keep();
  };
</script>

<form class="grid gap-y-4" onsubmit={(event) => void submit(event)}>
  <FaultNote fault={refused} />
  <Field
    label={t('parts.field.name')}
    control={Input}
    {...slash ? { error: t('parts.folder.slash') } : {}}
    controlProps={{
      name: 'name',
      required: true,
      'data-byte-limit': MAX_NAME,
      autocomplete: 'off',
      value: name,
      oninput: (event: Event) => {
        name = (event.currentTarget as HTMLInputElement).value;
      },
    }}
  />
  <div class="grid gap-y-2">
    <Label for="{uid}-parent">{t('parts.field.parent')}</Label>
    <FolderSelect
      id="{uid}-parent"
      value={parent}
      onValue={(next: number) => {
        parent = next;
      }}
      exclude={start.kind === 'change' ? start.folder.id : undefined}
    />
  </div>
  <div class="grid gap-y-2">
    <Label for="{uid}-description">{t('parts.field.folderDescription')}</Label>
    <Textarea
      id="{uid}-description"
      rows={3}
      data-byte-limit={MAX_FOLDER_DESCRIPTION}
      bind:value={description}
    />
  </div>
  <DialogFooter>
    <Button type="button" variant="secondary" onclick={onClose}>
      {t('parts.action.cancel')}
    </Button>
    <Button type="submit" disabled={!ready || catalogue.busy}>
      {start.kind === 'change'
        ? t('parts.action.save')
        : t('parts.action.create')}
    </Button>
  </DialogFooter>
</form>
