<!--
  Words a person wrote on the dashboard: a heading over a group of tiles, a
  reminder, the number to call. The text is drawn as text, line breaks kept,
  and never as markup, so nothing a person types can run on another
  person's screen.
-->
<script lang="ts">
  import StickyNoteIcon from '@lucide/svelte/icons/sticky-note';

  import { t } from '../../../i18n';
  import { EmptyState, TilePanel } from '../../../primitives';
  import { noteSettings, type NoteSize } from './config';
  import { NOTE_TONE } from './tones';
  import type { CardProps } from '../../../slots';

  let { config, title }: CardProps = $props();

  const settings = $derived(noteSettings(config));

  const SIZE: Readonly<Record<NoteSize, string>> = {
    sm: 'text-sm',
    md: 'text-base',
    lg: 'text-lg',
    xl: 'note-xl font-semibold',
  };
</script>

<div
  data-part="note"
  data-tone={settings.tone}
  class={[
    'flex min-h-0 flex-1 flex-col gap-2',
    settings.tone !== 'plain' && '-m-3 p-3',
    NOTE_TONE[settings.tone],
  ]}
>
  <TilePanel {title}>
    {#if settings.text.trim() === ''}
      <EmptyState
        size="sm"
        icon={StickyNoteIcon}
        heading={t('dashboard.note.empty')}
      />
    {:else}
      <div class="note-box min-h-0 flex-1">
        <p
          class={[
            'h-full overflow-y-auto break-words whitespace-pre-line',
            SIZE[settings.size],
            settings.align === 'center' &&
              'flex items-center justify-center text-center',
          ]}
        >
          {settings.text}
        </p>
      </div>
    {/if}
  </TilePanel>
</div>

<style>
  /* The box the words measure their tile by, in both directions. */
  .note-box {
    container-type: size;
  }

  /* A headline across a dashboard: as large as the tile allows, in width and
     in height, up to three times the theme's largest step. A headline in a
     tile one row tall stays inside it. */
  .note-xl {
    font-size: clamp(
      calc(var(--text-2xl) * 0.6),
      min(6cqi, 70cqb),
      calc(var(--text-2xl) * 3)
    );
    line-height: 1.15;
    text-wrap: balance;
  }
</style>
