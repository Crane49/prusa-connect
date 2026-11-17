<script lang="ts">
  // A module author's page: primitives from @prusa/connect-ui and a
  // client from @prusa/connect, with no meta-framework under either.
  //
  // It draws primitives and no shell. Merge 0bf80a81 removed the shell tier,
  // so `./primitives`, `./i18n` and `./tokens.css` are the subpaths a page
  // like this one reaches for.
  import { PrusaConnect } from "@prusa/connect";
  import { t } from "@prusa/connect-ui/i18n";
  import {
    Button,
    Card,
    CardContent,
    CardHeader,
    CardTitle,
  } from "@prusa/connect-ui/primitives";

  // Session mode (ADR-20260911-4p6 rule 20): the page the core serves carries the
  // cookie, so the client holds no token. It is built and never called here.
  // What this consumer proves is the install and the bundle, not that a
  // server answers.
  const local = new PrusaConnect({ auth: { kind: "session" } });

  // The transport keeps an empty origin when it was given none, because every
  // request then goes to a relative path against the page's own.
  const origin = local.url === "" ? window.location.origin : local.url;
</script>

<Card>
  <CardHeader>
    <CardTitle>
      <h1 class="contents">Fleet on {new URL(origin).host}</h1>
    </CardTitle>
  </CardHeader>
  <CardContent class="flex flex-col items-start gap-3">
    <p>A card and a button, drawn by the library's primitives.</p>

    <!-- The label is the library's own catalogue text, so the page reads the
         `./i18n` subpath as well as the `./primitives` one. -->
    <Button>{t("primitives.copy")}</Button>
  </CardContent>
</Card>
