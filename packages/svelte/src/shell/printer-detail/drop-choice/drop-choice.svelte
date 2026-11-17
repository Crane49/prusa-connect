<!--
  The question a file dropped on a printer asks before anything goes up:
  print it now, or only copy it to the printer. Confirm closes it, and the
  files go on their way where every other send is followed. The two choices
  are the core's own; adding a file to a queue needs a slot a module can fill
  here, with a callback and its own availability, which this does not have.

  A printer prints one file at a time, so several files are only copied, and a
  printer that cannot start a print now says why, in the words the print
  dialog uses for it. Cancel and closing the dialog send nothing.
-->
<script lang="ts">
  import { untrack } from 'svelte';
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
  import type { DropChoiceProps } from './types';

  let { names, printer, printTarget, onConfirm, onCancel }: DropChoiceProps =
    $props();

  const count = $derived(names?.length ?? 0);
  const printable = $derived(count === 1 && printTarget?.ready === true);

  // Why Print now is not offered, said under it in place of what it does.
  const blocked = $derived.by((): string | undefined => {
    if (printable) return undefined;
    if (count > 1) return t('printerDetailFiles.drop.oneAtATime');
    if (printTarget?.reasonKey === undefined) return undefined;

    return t(printTarget.reasonKey, {
      states: formatList(printTarget.reasonStates ?? []),
    });
  });

  // The pick, set when the dialog opens: Print now on a printer that can
  // print, copy otherwise. It is set once, so a printer that changes state
  // while the dialog is open does not move the pick under the person.
  let picked = $state<string | undefined>(undefined);
  $effect.pre(() => {
    if (names === undefined || picked !== undefined) return;
    picked = untrack(() => (printable ? 'print' : 'copy'));
  });
  const choice = $derived(picked ?? 'copy');

  // Print now picked on a printer that has since stopped being able to print
  // is not quietly turned into a copy: Confirm waits for another pick.
  const stale = $derived(choice === 'print' && !printable);

  const confirm = (): void => {
    if (stale) return;
    const print = choice === 'print';
    picked = undefined;
    onConfirm(print);
  };
</script>

<AlertDialog
  open={names !== undefined}
  onOpenChange={(open: boolean) => {
    if (open) return;
    picked = undefined;
    onCancel();
  }}
>
  <AlertDialogContent>
    <AlertDialogHeader>
      <AlertDialogTitle
        >{t('printerDetailFiles.drop.title', { printer })}</AlertDialogTitle
      >
      <AlertDialogDescription>
        {t('printerDetailFiles.drop.files', {
          count,
          names: formatList(names ?? []),
        })}
      </AlertDialogDescription>
    </AlertDialogHeader>
    <RadioGroup
      bind:value={
        () => choice,
        (next: string) => {
          picked = next;
        }
      }
      aria-label={t('printerDetailFiles.drop.title', { printer })}
      class="gap-3"
    >
      <Label variant="option" data-disabled={printable ? undefined : ''}>
        <RadioGroupItem value="print" disabled={!printable} class="mt-0.5" />
        <span class="flex flex-col gap-0.5">
          <span>{t('printerDetailFiles.drop.print')}</span>
          <span class="text-sm text-muted-foreground">
            {blocked ?? t('printerDetailFiles.drop.printNote')}
          </span>
        </span>
      </Label>
      <Label variant="option">
        <RadioGroupItem value="copy" class="mt-0.5" />
        <span class="flex flex-col gap-0.5">
          <span>{t('printerDetailFiles.drop.copy')}</span>
          <span class="text-sm text-muted-foreground">
            {t('printerDetailFiles.drop.copyNote')}
          </span>
        </span>
      </Label>
    </RadioGroup>
    <AlertDialogFooter>
      <AlertDialogCancel size="lg">{t('primitives.cancel')}</AlertDialogCancel>
      <AlertDialogAction size="lg" disabled={stale} onclick={confirm}>
        {t('printerDetailFiles.drop.confirm')}
      </AlertDialogAction>
    </AlertDialogFooter>
  </AlertDialogContent>
</AlertDialog>
