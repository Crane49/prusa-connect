// A page against this server, bundled by Vite with no framework. The lane
// builds it and type-checks it; it is not run in a browser there.
//
// The token is whatever the page was served with. In a real deployment the
// person signed in through /api/v1/auth/login and the page holds a credential
// the core minted; here it is read from the query string so the built page
// can be pointed at a server by hand.
import { ApiError, PrusaConnect } from "@prusa/connect";

const params = new URLSearchParams(window.location.search);
const url = params.get("url") ?? window.location.origin;
const auth = params.get("token") ?? "";

const local = new PrusaConnect({ url, auth });

const devices = document.getElementById("devices");
const events = document.getElementById("events");
if (devices === null || events === null) throw new Error("the page lost its lists");

const render = (list: HTMLElement, text: string): void => {
  const item = document.createElement("li");
  item.textContent = text;
  list.appendChild(item);
};

try {
  const page = await local.listDevices({ query: { limit: 100 } });
  for (const device of page.items ?? []) {
    render(devices, `${device.id} ${device.model} connected=${String(device.connected)}`);
  }
} catch (err) {
  render(devices, err instanceof ApiError ? `${err.status} ${err.code ?? ""} ${err.message}` : String(err));
}

const subscription = local.events({ types: ["com.prusa.connect.device.presence.changed"] });
window.addEventListener("pagehide", () => subscription.close());

for await (const event of subscription) {
  render(events, `${event.type} ${event.id ?? ""}`);
}
