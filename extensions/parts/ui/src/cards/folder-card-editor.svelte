<!--
  The settings of a folder card. The core's settings form would ask for the
  folder by its number, so the module draws its own: the folder from the
  tree, the tag from the tags in use, the order and how many parts.

  Every change hands back the whole settings object, and the dashboard holds
  it to the schema in module.yaml before it keeps it (ADR-20261008-hna rule 20).
-->
<script lang="ts">
  import {
    Input,
    Label,
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
    Switch,
    ToggleGroup,
    ToggleGroupItem,
  } from '@prusa/connect-ui/primitives';
  import type { CardEditorProps } from '@prusa/connect-ui/slots';
  import { onMount } from 'svelte';

  import FolderSelect from '../parts-page/folder-select.svelte';
  import { heldSession } from '../session';
  import { folderCardSettings } from './settings';

  let { config, onchange }: CardEditorProps = $props();

  const { host, catalogue } = heldSession();
  const t = host.t;
  const uid = $props.id();

  onMount(() => {
    void catalogue.frame();
  });

  const settings = $derived(folderCardSettings(config));

  const set = (key: string, value: unknown): void => {
    onchange({ ...config, [key]: value });
  };

  /** The empty tag, which keeps every part. Select takes no empty value. */
  const ANY = '*';
  const tags = $derived([
    { value: ANY, label: t('parts.dashboard.folder.anyTag') },
    ...catalogue.tags.map((tag) => ({ value: tag.tag, label: tag.tag })),
  ]);
</script>

<div class="grid gap-y-4" data-part="parts-folder-editor">
  <div class="grid gap-2">
    <Label for="{uid}-folder">{t('parts.dashboard.folder.folder')}</Label>
    <FolderSelect
      id="{uid}-folder"
      value={settings.folder}
      onValue={(folder) => {
        set('folder', folder);
      }}
    />
  </div>
  <div class="flex items-center justify-between gap-3">
    <Label for="{uid}-deep">{t('parts.dashboard.folder.deep')}</Label>
    <Switch
      id="{uid}-deep"
      checked={settings.deep}
      onCheckedChange={(on: boolean) => {
        set('deep', on);
      }}
    />
  </div>
  <div class="grid gap-2">
    <Label for="{uid}-tag">{t('parts.dashboard.folder.tag')}</Label>
    <Select
      value={settings.tag === '' ? ANY : settings.tag}
      items={tags}
      onValueChange={(next: string) => {
        set('tag', next === ANY ? '' : next);
      }}
    >
      <SelectTrigger id="{uid}-tag" class="w-full">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {#each tags as tag (tag.value)}
          <SelectItem value={tag.value} label={tag.label}
            >{tag.label}</SelectItem
          >
        {/each}
      </SelectContent>
    </Select>
  </div>
  <div class="grid gap-2">
    <Label id="{uid}-order">{t('parts.dashboard.folder.order')}</Label>
    <ToggleGroup
      type="single"
      variant="outline"
      size="sm"
      aria-labelledby="{uid}-order"
      class="justify-start"
      value={settings.order}
      onValueChange={(next: string) => {
        if (next === 'name' || next === 'changed') set('order', next);
      }}
    >
      <ToggleGroupItem value="changed"
        >{t('parts.order.changed')}</ToggleGroupItem
      >
      <ToggleGroupItem value="name">{t('parts.order.name')}</ToggleGroupItem>
    </ToggleGroup>
  </div>
  <div class="grid gap-2">
    <Label for="{uid}-limit">{t('parts.dashboard.folder.limit')}</Label>
    <Input
      id="{uid}-limit"
      type="number"
      inputmode="numeric"
      min={1}
      max={24}
      step={1}
      value={String(settings.limit)}
      onchange={(event: Event & { currentTarget: HTMLInputElement }) => {
        const next = event.currentTarget.valueAsNumber;
        if (Number.isInteger(next) && next >= 1 && next <= 24)
          set('limit', next);
      }}
    />
  </div>
</div>
