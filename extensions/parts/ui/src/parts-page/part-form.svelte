<!--
  The fields of the part dialog.

  Tags are typed as words separated by commas or spaces, the way a person
  writes them, and shown back as the module will keep them: lower case, each
  once. A refusal is drawn here, where the person can fix the field, and the
  dialog stays open until a write lands.
-->
<script lang="ts">
  import {
    Badge,
    Button,
    DialogFooter,
    Field,
    Input,
    Label,
    Textarea,
  } from '@prusa/connect-ui/primitives';
  import { untrack } from 'svelte';

  import {
    MAX_DESCRIPTION,
    MAX_NAME,
    MAX_SUMMARY,
    tagProblemOf,
    tagsOf,
  } from '../presentation';
  import type { PartDetail, PartDraft } from '../parts';
  import { heldSession } from '../session';
  import FaultNote from './fault-note.svelte';
  import FolderSelect from './folder-select.svelte';

  let {
    folder,
    part,
    onClose,
  }: { folder: number; part?: PartDetail | undefined; onClose: () => void } =
    $props();

  const { catalogue, navigation, host } = heldSession();
  const t = host.t;
  const uid = $props.id();

  // The form is made new each time the dialog opens, so the part it was made
  // for is the one it starts from.
  const editing = untrack(() => part);
  const startFolder = untrack(() => folder);
  let name = $state(editing?.name ?? '');
  let summary = $state(editing?.summary ?? '');
  let inside = $state(editing?.folder ?? startFolder);
  let tagText = $state((editing?.tags ?? []).join(', '));
  let description = $state('');
  let refused = $state<unknown>(undefined);

  const tags = $derived(tagsOf(tagText));
  const tagProblem = $derived(tagProblemOf(tags));
  const tagError = $derived.by(() => {
    if (tagProblem === undefined) return undefined;

    return tagProblem.kind === 'many'
      ? t('parts.field.tagsMany', { limit: tagProblem.limit })
      : t('parts.field.tagLong', {
          tag: tagProblem.tag,
          limit: tagProblem.limit,
        });
  });
  const ready = $derived(name.trim() !== '' && tagProblem === undefined);

  const typed = (set: (value: string) => void) => (event: Event) => {
    set((event.currentTarget as HTMLInputElement).value);
  };

  const submit = async (event: SubmitEvent): Promise<void> => {
    event.preventDefault();
    if (!ready || catalogue.busy) return;

    const draft: PartDraft = {
      name: name.trim(),
      summary: summary.trim(),
      folder: inside,
      tags,
    };

    if (editing === undefined) {
      if (description.trim() !== '') draft.description = description.trim();
      const made = await catalogue.addPart(draft);
      if (made === undefined) {
        refused = catalogue.fault;
        catalogue.dismiss();

        return;
      }
      onClose();
      navigation.openPart(made.id, made.folder);

      return;
    }

    if (await catalogue.changePart(editing.id, draft)) {
      onClose();
      // A part moved to another folder is shown in the folder it is in now.
      if (inside !== editing.folder) navigation.openPart(editing.id, inside);
    } else {
      refused = catalogue.fault;
      catalogue.dismiss();
    }
  };
</script>

<form class="grid gap-y-4" onsubmit={(event) => void submit(event)}>
  <FaultNote fault={refused} />
  <Field
    label={t('parts.field.name')}
    control={Input}
    controlProps={{
      name: 'name',
      required: true,
      'data-byte-limit': MAX_NAME,
      autocomplete: 'off',
      value: name,
      oninput: typed((value) => (name = value)),
    }}
  />
  <Field
    label={t('parts.field.summary')}
    description={t('parts.field.summaryHelp')}
    control={Input}
    controlProps={{
      name: 'summary',
      'data-byte-limit': MAX_SUMMARY,
      autocomplete: 'off',
      value: summary,
      oninput: typed((value) => (summary = value)),
    }}
  />
  <div class="grid gap-y-2">
    <Label for="{uid}-folder">{t('parts.field.folder')}</Label>
    <FolderSelect
      id="{uid}-folder"
      value={inside}
      onValue={(next: number) => {
        inside = next;
      }}
    />
  </div>
  <Field
    label={t('parts.field.tags')}
    description={t('parts.field.tagsHelp')}
    {...tagError === undefined ? {} : { error: tagError }}
    control={Input}
    controlProps={{
      name: 'tags',
      autocomplete: 'off',
      value: tagText,
      oninput: typed((value) => (tagText = value)),
    }}
  />
  {#if tags.length > 0}
    <span class="-mt-2 flex flex-wrap gap-1" aria-hidden="true">
      {#each tags as tag (tag)}
        <Badge variant="outline">{tag}</Badge>
      {/each}
    </span>
  {/if}
  {#if editing === undefined}
    <div class="grid gap-y-2">
      <Label for="{uid}-description">{t('parts.field.description')}</Label>
      <Textarea
        id="{uid}-description"
        rows={5}
        data-byte-limit={MAX_DESCRIPTION}
        placeholder={t('parts.field.descriptionPlaceholder')}
        bind:value={description}
      />
    </div>
  {/if}
  <DialogFooter>
    <Button type="button" variant="secondary" onclick={onClose}>
      {t('parts.action.cancel')}
    </Button>
    <Button
      type="submit"
      disabled={!ready || catalogue.busy}
      data-part="save-part"
    >
      {editing === undefined
        ? t('parts.action.create')
        : t('parts.action.save')}
    </Button>
  </DialogFooter>
</form>
