// The consumer `make client-sdk-install-check` runs. It is what a person's
// first script against this server looks like: install the package, point it
// at a server, list the printers, read the stream.
//
// It exits 0 once it has seen at least one device and one event, and 1 with a
// line naming what it did not see. PCL_URL is the server; PCL_TOKEN is the
// credential. The lane mints one against the demo account `cmd/demo` serves,
// and it may be empty only against a server that authenticates nobody.
import { ApiError, PrusaConnect, NetworkError } from "@prusa/connect";

const url = process.env["PCL_URL"] ?? "http://127.0.0.1:8080";
const auth = process.env["PCL_TOKEN"] ?? "";

// How long to wait for the first event before giving up. The demo publishes
// telemetry every few seconds, so this is generous.
const EVENT_BUDGET_MS = 30_000;

const local = new PrusaConnect({ url, auth });

const page = await local.listDevices({ query: { limit: 100 } });
const devices = page.items ?? [];
for (const device of devices) {
  console.log(`device ${device.id} ${device.model} connected=${device.connected}`);
}
if (devices.length === 0) {
  console.error("FAIL  the server lists no device");
  process.exit(1);
}

const controller = new AbortController();
const timer = setTimeout(() => controller.abort(), EVENT_BUDGET_MS);
const events = local.events({ signal: controller.signal });

let seen = 0;
try {
  for await (const event of events) {
    console.log(`event ${event.type} ${event.id ?? "-"}`);
    seen += 1;
    break;
  }
} catch (err) {
  if (err instanceof ApiError) console.error(`FAIL  the stream refused: ${err.status} ${err.code ?? ""}`);
  else if (err instanceof NetworkError) console.error(`FAIL  the stream got no answer: ${err.message}`);
  else throw err;
  process.exit(1);
} finally {
  clearTimeout(timer);
  events.close();
}

if (seen === 0) {
  console.error(`FAIL  no event arrived within ${EVENT_BUDGET_MS} ms`);
  process.exit(1);
}

console.log(`OK    ${devices.length} device(s) listed, ${seen} event read`);
