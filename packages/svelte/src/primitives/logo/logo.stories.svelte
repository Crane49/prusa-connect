<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import Logo from './logo.svelte';

  const { Story } = defineMeta({
    title: 'Primitives/Logo',
    component: Logo,
  });
</script>

<Story name="Default" args={{ size: 'lg' }} />

<!-- The three sizes, on the same left edge the lockup aligns to. -->
<Story name="Sizes" asChild>
  <div class="flex flex-col items-start gap-6">
    <Logo size="sm" />
    <Logo size="md" />
    <Logo size="lg" />
  </div>
</Story>

<!-- The point of the mark: one shape, two grounds. Nothing here sets a colour
     on the Logo itself. Each panel sets a text colour and the mark inherits
     it, which is what keeps it a black-or-white lockup rather than a palette. -->
<Story name="Black And White" asChild>
  <div class="flex flex-wrap gap-4">
    <div class="flex-1 rounded-sm bg-white p-8 text-black">
      <Logo size="lg" />
    </div>
    <div class="flex-1 rounded-sm bg-black p-8 text-white">
      <Logo size="lg" />
    </div>
  </div>
</Story>

<!-- Over a filled surface, to show the mark is not tied to the page's ground. -->
<Story name="On A Coloured Ground" asChild>
  <div class="bg-primary p-8 text-primary-foreground">
    <Logo size="lg" />
  </div>
</Story>

<!-- The narrow shape: one word per line, each justified to the mark like the
     line above it. A phone's brand slot has height to spare and no width. -->
<Story name="Three Lines" asChild>
  <div class="flex flex-wrap items-start gap-10">
    <Logo size="sm" lines="three" />
    <Logo size="md" lines="three" />
    <Logo size="lg" lines="three" />
  </div>
</Story>

<!-- No subtitle: the bare company mark, for a slot too small for two lines. -->
<Story name="Wordmark Only" args={{ size: 'lg', subtitle: '' }} />

<!-- The name is a prop, so a build that is not Prusa Connect can say so. -->
<Story name="Custom Subtitle" args={{ size: 'lg', subtitle: 'Connect' }} />

<!-- The lockup's own rule: the name is as wide as the mark above it, to within
     a few percent at every size, by its size alone. The container stretches on
     purpose: without `w-fit` on the root the mark would take this column's
     width instead. -->
<Story name="Name Matches The Mark Width" asChild>
  <div class="flex w-96 flex-col gap-8">
    <Logo size="sm" />
    <Logo size="md" />
    <Logo size="lg" />
  </div>
</Story>

<!-- The two things this component promises: it is real text, and it inherits
     its colour rather than declaring one. -->
<Story name="Is Text And Inherits Colour" asChild>
  <div class="text-black">
    <Logo size="lg" />
  </div>
</Story>
