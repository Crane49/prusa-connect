import { expect, test } from "vitest";
import { page } from "vitest/browser";
import { render } from "vitest-browser-svelte";

import App from "./App.svelte";

test("a primitive draws in a real browser", async () => {
  await render(App);

  // The button came from the installed package and not from this project: it
  // carries the `data-slot` the primitive writes, and its label is a line the
  // library's own catalogue holds.
  const button = page.getByRole("button", { name: "Copy" });
  await expect.element(button).toBeInTheDocument();
  expect(button.element().dataset.slot).toBe("button");

  // The card around it, so the page proves a composed primitive and not one
  // element alone.
  await expect
    .element(page.getByRole("heading", { name: /^Fleet on / }))
    .toBeInTheDocument();
});
