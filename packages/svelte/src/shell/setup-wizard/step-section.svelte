<!--
  One section of the configuration. A key that turns others on is drawn with
  them under it, and they show only while it is on.
-->
<script lang="ts">
  import type { SetupConfiguration, SetupKey } from '../../data';
  import ConfigField from './config-field.svelte';
  import MqttBlock from './mqtt-block.svelte';
  import { type Answer, isActive, parentOf, shownOf } from './model';
  import type { SetupDraft } from './setup-draft.svelte';

  interface StepSectionProps {
    draft: SetupDraft;
    contract: SetupConfiguration;
    keys: readonly SetupKey[];
    file: string;
    docsHref: string | undefined;
    showErrors: boolean;
    /** Whether the section ends with the MQTT broker. */
    mqtt?: boolean;
    /** The documentation page about MQTT. */
    mqttGuide?: string | undefined;
  }

  let {
    draft,
    contract,
    keys,
    file,
    docsHref,
    showErrors,
    mqtt = false,
    mqttGuide,
  }: StepSectionProps = $props();

  const shownPaths = $derived(new Set(keys.map((key) => key.path)));
  // A key whose switch is drawn in this section goes under it. One whose
  // switch is elsewhere stands on its own, and shows while that switch is on.
  const roots = $derived(
    keys.filter((key) => {
      const parent = parentOf(key);

      return parent === undefined || !shownPaths.has(parent);
    }),
  );
  const childrenOf = (path: string): SetupKey[] =>
    keys.filter((key) => parentOf(key) === path);
</script>

{#snippet field(key: SetupKey)}
  <ConfigField
    {key}
    {file}
    {docsHref}
    shown={shownOf(key, draft.answers)}
    answered={draft.answers[key.path] !== undefined}
    showError={showErrors}
    onanswer={(value: Answer) => {
      draft.answer(key.path, value);
    }}
    onforget={() => {
      draft.forget(key.path);
    }}
  />
{/snippet}

{#snippet branch(key: SetupKey)}
  {@const children = childrenOf(key.path)}
  {@render field(key)}
  {#if children.some((child) => isActive(child, contract, draft.answers))}
    <div
      class="flex flex-col gap-5 border-s-2 border-primary/40 ps-4"
      data-part="dependents"
      data-of={key.path}
    >
      {#each children as child (child.path)}
        {#if isActive(child, contract, draft.answers)}
          {@render branch(child)}
        {/if}
      {/each}
    </div>
  {/if}
{/snippet}

<div class="flex flex-col gap-5" data-part="section">
  {#each roots as key (key.path)}
    {#if isActive(key, contract, draft.answers)}
      {@render branch(key)}
    {/if}
  {/each}
  {#if mqtt}
    <MqttBlock {draft} {showErrors} guide={mqttGuide} />
  {/if}
</div>
