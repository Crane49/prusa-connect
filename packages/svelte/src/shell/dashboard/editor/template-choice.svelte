<!--
  The templates a new dashboard can start from, as a group of radio cards:
  each with its drawing, its name and one line on what it is for. Arrow keys
  walk the group, as they walk any radio group (primitives/radio-group). The
  name stretches over its card, so a press anywhere on the card picks it.
-->
<script lang="ts">
  import { DASHBOARD_TEMPLATES, type DashboardTemplate } from '../../../domain';
  import { t } from '../../../i18n';
  import { Label, RadioGroup, RadioGroupItem } from '../../../primitives';
  import TemplatePicture from './template-picture.svelte';

  let {
    value,
    onchange,
  }: { value: DashboardTemplate; onchange: (next: DashboardTemplate) => void } =
    $props();
  const uid = $props.id();

  const NAME_KEY: Readonly<Record<DashboardTemplate, string>> = {
    blank: 'dashboard.template.blank.name',
    farm: 'dashboard.template.farm.name',
    printer: 'dashboard.template.printer.name',
    wall: 'dashboard.template.wall.name',
  };

  const ABOUT_KEY: Readonly<Record<DashboardTemplate, string>> = {
    blank: 'dashboard.template.blank.about',
    farm: 'dashboard.template.farm.about',
    printer: 'dashboard.template.printer.about',
    wall: 'dashboard.template.wall.about',
  };

  const picked = (next: string): void => {
    const template = DASHBOARD_TEMPLATES.find((known) => known === next);
    if (template !== undefined) onchange(template);
  };
</script>

<div class="grid gap-2">
  <span id="{uid}-legend" class="text-sm font-medium"
    >{t('dashboard.template.legend')}</span
  >
  <RadioGroup
    {value}
    onValueChange={picked}
    aria-labelledby="{uid}-legend"
    class="grid-cols-2"
  >
    {#each DASHBOARD_TEMPLATES as template (template)}
      <div
        data-part="template"
        class="relative flex flex-col gap-2 rounded-md border border-border p-2 hover:bg-accent has-aria-checked:border-selected has-aria-checked:bg-selected/10"
      >
        <TemplatePicture {template} />
        <span class="flex items-center gap-2">
          <RadioGroupItem
            id="{uid}-{template}"
            value={template}
            aria-describedby="{uid}-{template}-about"
          />
          <Label
            for="{uid}-{template}"
            class="cursor-pointer after:absolute after:inset-0"
            >{t(NAME_KEY[template])}</Label
          >
        </span>
        <span id="{uid}-{template}-about" class="text-xs text-muted-foreground"
          >{t(ABOUT_KEY[template])}</span
        >
      </div>
    {/each}
  </RadioGroup>
</div>
