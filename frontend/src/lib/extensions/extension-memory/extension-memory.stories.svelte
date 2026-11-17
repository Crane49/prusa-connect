<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { noop } from '../../../../test/extension-fixtures';
  import ExtensionMemory from './extension-memory.svelte';

  const { Story } = defineMeta({
    title: 'Parts/Extensions/Extension Memory',
    component: ExtensionMemory,
  });

  const written = (): Promise<boolean> => Promise.resolve(true);

  const estimate = {
    mib: 256,
    source: 'author',
    overPool: false,
    committedMiB: 1024,
    poolMiB: 3072,
  } as const;
</script>

<Story name="Stop" asChild>
  <ExtensionMemory
    {estimate}
    usedBytes={96 * 1_048_576}
    overBudget={false}
    onOverMemory="stop"
    saving={false}
    onchange={noop}
    onbudget={written}
  />
</Story>

<Story name="Warn" asChild>
  <ExtensionMemory
    {estimate}
    usedBytes={96 * 1_048_576}
    overBudget={false}
    onOverMemory="warn"
    saving={false}
    onchange={noop}
    onbudget={written}
  />
</Story>

<Story name="Near Budget" asChild>
  <ExtensionMemory
    {estimate}
    usedBytes={220 * 1_048_576}
    overBudget={false}
    nearBudget
    onOverMemory="stop"
    saving={false}
    onchange={noop}
    onbudget={written}
  />
</Story>

<Story name="Running Over Budget" asChild>
  <ExtensionMemory
    {estimate}
    usedBytes={300 * 1_048_576}
    overBudget
    onOverMemory="warn"
    saving={false}
    onchange={noop}
    onbudget={written}
  />
</Story>

<Story name="Usage Unknown" asChild>
  <ExtensionMemory
    {estimate}
    overBudget={false}
    onOverMemory="stop"
    saving={false}
    onchange={noop}
    onbudget={written}
  />
</Story>

<Story name="Server Default Budget" asChild>
  <ExtensionMemory
    estimate={{ ...estimate, mib: 128, source: 'default' }}
    usedBytes={8 * 1_048_576}
    overBudget={false}
    onOverMemory="stop"
    saving={false}
    onchange={noop}
    onbudget={written}
  />
</Story>

<Story name="Over Pool" asChild>
  <ExtensionMemory
    estimate={{ ...estimate, mib: 2560, overPool: true, committedMiB: 3584 }}
    overBudget={false}
    onOverMemory="stop"
    saving={false}
    onchange={noop}
    onbudget={written}
  />
</Story>

<Story name="Locked" asChild>
  <ExtensionMemory
    {estimate}
    overBudget={false}
    onOverMemory="stop"
    locked="The configuration file sets this module."
    saving={false}
    onchange={noop}
    onbudget={written}
  />
</Story>

<Story name="Operator Budget" asChild>
  <ExtensionMemory
    estimate={{ ...estimate, mib: 512, source: 'operator' }}
    overBudget={false}
    onOverMemory="stop"
    budgetMiB={512}
    recommendedMiB={256}
    running
    saving={false}
    onchange={noop}
    onbudget={written}
  />
</Story>

<Story name="Author Recommendation" asChild>
  <ExtensionMemory
    {estimate}
    overBudget={false}
    onOverMemory="stop"
    recommendedMiB={256}
    saving={false}
    onchange={noop}
    onbudget={written}
  />
</Story>

<Story
  name="Invalid Budget"
  asChild
  play={async ({ canvas, userEvent }) => {
    await userEvent.type(canvas.getByRole('spinbutton'), '9000');
  }}
>
  <ExtensionMemory
    {estimate}
    overBudget={false}
    onOverMemory="stop"
    saving={false}
    onchange={noop}
    onbudget={written}
  />
</Story>

<Story name="Switch-off Server Default" asChild>
  <ExtensionMemory
    {estimate}
    overBudget={false}
    onOverMemory="stop"
    saving={false}
    onchange={noop}
    onbudget={written}
    switchOff={{ failures: 3, windowSeconds: 3600, source: 'default' }}
    onswitchoff={written}
  />
</Story>

<Story name="Switch-off Never" asChild>
  <ExtensionMemory
    {estimate}
    overBudget={false}
    onOverMemory="stop"
    saving={false}
    onchange={noop}
    onbudget={written}
    switchOff={{ failures: 0, windowSeconds: 21600, source: 'operator' }}
    onswitchoff={written}
  />
</Story>

<Story name="Narrow" asChild>
  <div class="w-full max-w-90">
    <ExtensionMemory
      estimate={{ ...estimate, mib: 512, source: 'operator' }}
      usedBytes={420 * 1_048_576}
      overBudget={false}
      nearBudget
      onOverMemory="warn"
      budgetMiB={512}
      recommendedMiB={256}
      running
      saving={false}
      onchange={noop}
      onbudget={written}
      switchOff={{ failures: 3, windowSeconds: 3600, source: 'operator' }}
      onswitchoff={written}
    />
  </div>
</Story>
