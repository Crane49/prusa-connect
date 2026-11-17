import { mount } from "svelte";

import Fleet from "./Fleet.svelte";

const target = document.getElementById("app");
if (target === null) throw new Error("the page has no #app");

// The token is whatever the page was served with; here it is read from the
// query string so the built page can be pointed at a server by hand.
const params = new URLSearchParams(window.location.search);

mount(Fleet, {
  target,
  props: { url: params.get("url") ?? window.location.origin, auth: params.get("token") ?? "" },
});
