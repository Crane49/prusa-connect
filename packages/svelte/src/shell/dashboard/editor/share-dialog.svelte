<!--
  The two links of a dashboard: the one that opens it in the app, and the one
  that fills a screen with it.

  A link opens nothing for a person who is not signed in, so the dialog says
  who can open it: anybody signed in whom its visibility lets in. A screen on a
  wall is signed in once, with an account that may only look.
-->
<script lang="ts">
  import {
    CopyField,
    Dialog,
    DialogContent,
    DialogDescription,
    DialogHeader,
    DialogTitle,
  } from '../../../primitives';

  import { t } from '../../../i18n';
  import { dashboardContext } from '../context';

  let {
    dashboard,
    onClose,
  }: { dashboard: number | undefined; onClose: () => void } = $props();

  const { navigation } = dashboardContext();

  const link = $derived(
    dashboard === undefined ? '' : navigation.link(dashboard, false),
  );
  const wall = $derived(
    dashboard === undefined ? '' : navigation.link(dashboard, true),
  );
</script>

<Dialog
  open={dashboard !== undefined}
  onOpenChange={(next: boolean) => {
    if (!next) onClose();
  }}
>
  <DialogContent>
    <DialogHeader>
      <DialogTitle>{t('dashboard.share.title')}</DialogTitle>
      <DialogDescription>{t('dashboard.share.note')}</DialogDescription>
    </DialogHeader>
    <div class="grid gap-y-4" data-part="share">
      <CopyField label={t('dashboard.share.link')} value={link} />
      <div class="grid gap-y-2">
        <CopyField label={t('dashboard.share.wall')} value={wall} />
        <p class="text-xs text-muted-foreground">
          {t('dashboard.share.wallNote')}
        </p>
      </div>
    </div>
  </DialogContent>
</Dialog>
