// The page protocol of PrusaSlicer 3: the messages a page shown in the
// slicer's webview sends and the calls it hears (ADR-20261112-2eb rules 4
// and 5). It has its own subpath, apart from the slot registry, because it
// is the slicer's contract and not this server's.
export {
  type CompatiblePrinterParams,
  createSlicer,
  type Slicer,
  slicer,
  type SlicerEventDetail,
  type SlicerEventName,
  type SlicerEvents,
  type SlicerHost,
  type SlicerInitOptions,
  type SlicerMessage,
  type SlicerPrintData,
  type SlicerWindow,
} from './slicer';
