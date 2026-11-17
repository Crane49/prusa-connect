// Package devtools is the developer view of a running core: which connectors
// this build carries and where they are mounted, and the commands this process
// carried to a printer.
//
// It answers "what is actually happening" for somebody at a bench with a real
// printer on the other end of a cable. Everything it reports is either build
// metadata or a rendering of what the caller may already read through
// /api/v1/devices, /api/v1/readiness and /api/v1/events, so it holds no
// authority of its own and adds nothing to the printer frame path: the live
// wire the page shows is the event hub's own stream, subscribed to the way any
// other reader subscribes.
package devtools
