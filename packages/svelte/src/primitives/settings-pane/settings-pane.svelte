<script lang="ts">
  // The shape every settings tab uses: a heading, one line of description, the
  // rows, and a band at the foot holding what saves them (earlier research
  // recommendation 6).
  //
  // It is painted rather than outlined, and it takes the
  // card surface, because a screen that mixes panes and cards reads as one
  // surface at one height.
  import type { SettingsPaneProps } from './types';

  let {
    title,
    description,
    children,
    actions,
    message,
    messageIsFailure = false,
    edged = false,
    class: className,
  }: SettingsPaneProps = $props();

  // The heading names the section, so the section points at it and a screen
  // reader announces the pane by its own title.
  const headingId = $props.id();
</script>

<section
  data-slot="settings-pane"
  aria-labelledby={headingId}
  class={[
    'rounded-panel @container/pane flex flex-col bg-card text-card-foreground',
    edged && 'border border-border',
    className,
  ]}
>
  <div class="flex flex-col gap-1 p-4 pb-2 @md/pane:p-6 @md/pane:pb-3">
    <h2 id={headingId} class="text-lead font-semibold">{title}</h2>
    {#if description}
      <p class="text-body text-muted-foreground">{description}</p>
    {/if}
  </div>

  <div
    data-slot="settings-pane-body"
    class="flex flex-col px-4 pb-4 @md/pane:px-6 @md/pane:pb-6"
  >
    {@render children?.()}
  </div>

  {#if message}
    <p
      data-slot="settings-pane-message"
      role={messageIsFailure ? 'alert' : 'status'}
      class={[
        'text-body px-4 pb-3 @md/pane:px-6',
        messageIsFailure ? 'text-destructive-text' : 'text-muted-foreground',
      ]}
    >
      {message}
    </p>
  {/if}

  {#if actions}
    <!-- The band sits back from the pane by a tone of its own, so the rule
         over it is the seam between two different paints and not a line drawn
         for its own sake. -->
    <div
      data-slot="settings-pane-actions"
      class="bg-tone-sunken text-tone-sunken-foreground flex flex-col-reverse gap-2 border-t border-border p-4 @xs/pane:flex-row @xs/pane:items-center @xs/pane:justify-end @md/pane:p-6"
    >
      {@render actions()}
    </div>
  {/if}
</section>
