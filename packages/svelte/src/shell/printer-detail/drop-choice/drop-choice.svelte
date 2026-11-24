<!--
  The question a file dropped on a printer asks before anything goes up:
  print it now, or only copy it to the printer. Confirm closes it, and the
  files go on their way where every other send is followed. Each action a
  module adds in the device-upload-action slot, such as Add to queue, is a
  choice too: the file is copied, and the action runs while it is on its way.

  A printer prints one file at a time, so of several files the person picks
  the one to print, and the rest are only copied. Files of which none is a
  print file, such as a firmware image or a model, offer no print. One
  firmware image alone can be flashed once it lands, on a printer that can
  flash now, and the flash is picked when the dialog opens. A printer that
  cannot start a print now says why, in the words the print dialog uses for
  it. Cancel and closing the dialog send nothing.

  Files a module sent open it with that module's choice picked, and ask here
  whether to keep a copy in a storage, which the page asks beside its drop
  zone for a file dropped there.
-->
<script lang="ts">
  import { untrack } from 'svelte';
  import { isPrintFile } from '../../../domain';
  import { formatList, t } from '../../../i18n';
  import AlertDialog from '../../../primitives/alert-dialog/alert-dialog.svelte';
  import AlertDialogAction from '../../../primitives/alert-dialog/alert-dialog-action.svelte';
  import AlertDialogCancel from '../../../primitives/alert-dialog/alert-dialog-cancel.svelte';
  import AlertDialogContent from '../../../primitives/alert-dialog/alert-dialog-content.svelte';
  import AlertDialogDescription from '../../../primitives/alert-dialog/alert-dialog-description.svelte';
  import AlertDialogFooter from '../../../primitives/alert-dialog/alert-dialog-footer.svelte';
  import AlertDialogHeader from '../../../primitives/alert-dialog/alert-dialog-header.svelte';
  import AlertDialogTitle from '../../../primitives/alert-dialog/alert-dialog-title.svelte';
  import { Label } from '../../../primitives/label';
  import { RadioGroup, RadioGroupItem } from '../../../primitives/radio-group';
  import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
  } from '../../../primitives/select';
  import { isFirmwareName } from '../files/presentation';
  import { SaveToStorage } from '../save-to-storage';
  import type { DropChoiceProps } from './types';

  let {
    names,
    printer,
    printTarget,
    actions = [],
    preset,
    keep,
    onConfirm,
    onFlash,
    onCancel,
  }: DropChoiceProps = $props();

  const keyOf = (action: { namespace: string; id: string }): string =>
    `action:${action.namespace}.${action.id}`;

  const count = $derived(names?.length ?? 0);
  const several = $derived(count > 1);
  // The places of the files a printer can start a job from: a firmware image
  // or a model is only ever copied.
  const printFiles = $derived(
    (names ?? []).flatMap((name, at) => (isPrintFile(name) ? [at] : [])),
  );
  const printable = $derived(
    printFiles.length > 0 && printTarget?.ready === true,
  );
  const firmwareAlone = $derived(
    count === 1 && isFirmwareName(names?.[0] ?? ''),
  );
  const flashable = $derived(firmwareAlone && onFlash !== undefined);

  // Why Print now is not offered, said under it in place of what it does.
  const blocked = $derived.by((): string | undefined => {
    if (printable) return undefined;
    if (printTarget?.reasonKey === undefined) return undefined;

    return t(printTarget.reasonKey, {
      states: formatList(printTarget.reasonStates ?? []),
    });
  });

  // The pick, set when the dialog opens: Print now on a printer that can
  // print, Flash for a firmware image it can flash, copy otherwise. It is set
  // once, so a printer that changes state while the dialog is open does not
  // move the pick under the person.
  let picked = $state<string | undefined>(undefined);
  // Whether Flash was offered when the dialog opened. It stays offered,
  // greyed once the printer stops being able to flash, so the choices do not
  // vanish under the person.
  let offeredFlash = $state(false);
  $effect.pre(() => {
    if (names === undefined || picked !== undefined) return;
    picked = untrack(() => {
      if (preset !== undefined) return keyOf(preset);

      if (printable) return 'print';

      return flashable ? 'flash' : 'copy';
    });
    offeredFlash = untrack(() => flashable);
  });
  const choice = $derived(picked ?? 'copy');

  // Which of several files to print, by its place among the files dropped,
  // so two files of the same name stay two. The first print file until the
  // person picks another.
  let chosen = $state<number | undefined>(undefined);
  const printing = $derived(
    chosen !== undefined && printFiles.includes(chosen)
      ? chosen
      : (printFiles[0] ?? 0),
  );

  // Flash stays in sight, greyed, once offered on a printer that stopped
  // being able to flash, so the person can pick the copy instead.
  const flashShown = $derived(flashable || (firmwareAlone && offeredFlash));

  // Nothing to print or flash and no module's choice leaves the copy alone,
  // so there is nothing to pick and the button says what it does.
  const copyAlone = $derived(
    printFiles.length === 0 && actions.length === 0 && !flashShown,
  );

  // Print now or Flash picked on a printer that has since stopped being able
  // to is not quietly turned into a copy: Confirm waits for another pick.
  const stale = $derived(
    (choice === 'print' && !printable) || (choice === 'flash' && !flashable),
  );

  const confirm = (): void => {
    if (stale) return;
    if (choice === 'flash') {
      picked = undefined;
      offeredFlash = false;
      onFlash?.();

      return;
    }
    const print = choice === 'print' ? printing : undefined;
    const action = actions.find((one) => keyOf(one) === choice);
    picked = undefined;
    chosen = undefined;
    offeredFlash = false;
    onConfirm(print, action);
  };
</script>

<AlertDialog
  open={names !== undefined}
  onOpenChange={(open: boolean) => {
    if (open) return;
    picked = undefined;
    chosen = undefined;
    offeredFlash = false;
    onCancel();
  }}
>
  <AlertDialogContent>
    <AlertDialogHeader>
      <AlertDialogTitle
        >{t('printerDetailFiles.drop.title', {
          count,
          printer,
        })}</AlertDialogTitle
      >
      <AlertDialogDescription>
        {#if count === 1}
          {names?.[0]}
        {:else}
          <!-- One name a line; the title says how many. -->
          <ul class="flex flex-col gap-0.5">
            {#each names ?? [] as name, at (at)}
              <li>{name}</li>
            {/each}
          </ul>
        {/if}
      </AlertDialogDescription>
    </AlertDialogHeader>
    {#if !copyAlone}
      <RadioGroup
        bind:value={
          () => choice,
          (next: string) => {
            picked = next;
          }
        }
        aria-label={t('printerDetailFiles.drop.title', { count, printer })}
        class="gap-3"
      >
        {#if printFiles.length > 0}
          <Label variant="option" data-disabled={printable ? undefined : ''}>
            <RadioGroupItem
              value="print"
              disabled={!printable}
              class="mt-0.5"
            />
            <span class="flex min-w-0 flex-1 flex-col gap-0.5">
              <span>
                {t(
                  several
                    ? 'printerDetailFiles.drop.printOne'
                    : 'printerDetailFiles.drop.print',
                )}
              </span>
              <span class="text-sm text-muted-foreground">
                {blocked ??
                  t(
                    several
                      ? 'printerDetailFiles.drop.printOneNote'
                      : 'printerDetailFiles.drop.printNote',
                  )}
              </span>
            </span>
          </Label>
        {/if}
        {#if several && printable}
          <!-- Indented by the radio and the gap beside it, so the list lines up
             with the option's text. -->
          <div class="min-w-0 pl-6">
            <!-- Outside the option's label, so opening the list does not pick
               the option a second time. It stays while Only copy is picked,
               greyed, so the dialog keeps its height between the two. -->
            <Select
              disabled={choice !== 'print'}
              value={String(printing)}
              items={printFiles.map((at) => ({
                value: String(at),
                label: names?.[at] ?? '',
              }))}
              onValueChange={(next: string) => {
                chosen = Number(next);
              }}
            >
              <SelectTrigger
                class="max-w-full min-w-0 font-mono"
                aria-label={t('printerDetailFiles.drop.printWhich')}
              >
                <!-- Its own span and not SelectValue, which the trigger lays out as
                   a flex row that clips a long name without an ellipsis. -->
                <span class="min-w-0 truncate">{names?.[printing]}</span>
              </SelectTrigger>
              <!-- As wide as the trigger, so a long name wraps on a phone instead
                 of running past the screen. -->
              <SelectContent class="max-w-(--radix-select-trigger-width)">
                {#each printFiles as at (at)}
                  <SelectItem
                    value={String(at)}
                    label={names?.[at] ?? ''}
                    class="wrap-anywhere"
                  >
                    {names?.[at]}
                  </SelectItem>
                {/each}
              </SelectContent>
            </Select>
          </div>
        {/if}
        {#if flashShown}
          <Label variant="option" data-disabled={flashable ? undefined : ''}>
            <RadioGroupItem
              value="flash"
              disabled={!flashable}
              class="mt-0.5"
            />
            <span class="flex min-w-0 flex-1 flex-col gap-0.5">
              <span>{t('printerDetailFiles.drop.flash')}</span>
              <span class="text-sm text-muted-foreground">
                {t(
                  flashable
                    ? 'printerDetailFiles.drop.flashNote'
                    : 'printerDetailFiles.drop.flashBlocked',
                )}
              </span>
            </span>
          </Label>
        {/if}
        <Label variant="option">
          <RadioGroupItem value="copy" class="mt-0.5" />
          <span class="flex min-w-0 flex-1 flex-col gap-0.5">
            <span>
              {t(
                printFiles.length > 0 || flashShown
                  ? 'printerDetailFiles.drop.copy'
                  : 'printerDetailFiles.drop.copyNoPrint',
              )}
            </span>
            <!-- Not printed or not flashed says something only beside a
                 print or a flash to pick. -->
            {#if printFiles.length > 0}
              <span class="text-sm text-muted-foreground">
                {t('printerDetailFiles.drop.copyNote', { count })}
              </span>
            {:else if flashShown}
              <span class="text-sm text-muted-foreground">
                {t('printerDetailFiles.drop.copyNotFlashedNote')}
              </span>
            {/if}
          </span>
        </Label>
        {#each actions as action (keyOf(action))}
          <Label variant="option">
            <RadioGroupItem value={keyOf(action)} class="mt-0.5" />
            <span
              class="flex flex-col gap-0.5"
              data-slot="device-upload-action"
              data-module={action.namespace}
              data-section={action.id}
            >
              <span>{action.title}</span>
              <span class="text-sm text-muted-foreground">
                {t('printerDetailFiles.drop.actionNote')}
              </span>
            </span>
          </Label>
        {/each}
      </RadioGroup>
    {/if}
    {#if keep}
      <SaveToStorage {...keep} />
    {/if}
    <AlertDialogFooter>
      <AlertDialogCancel size="lg">{t('primitives.cancel')}</AlertDialogCancel>
      <AlertDialogAction size="lg" disabled={stale} onclick={confirm}>
        {t(
          copyAlone
            ? 'printerDetailFiles.drop.copyNoPrint'
            : 'printerDetailFiles.drop.confirm',
        )}
      </AlertDialogAction>
    </AlertDialogFooter>
  </AlertDialogContent>
</AlertDialog>
