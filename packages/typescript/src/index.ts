export { PrusaConnect, type PrusaConnectOptions } from "./client.js";
export { ApiError, NetworkError, isProblem, type Problem } from "./errors.js";
export {
  HttpTransport,
  type Auth,
  type BearerAuth,
  type SessionAuth,
  type TokenProvider,
  type TransportOptions,
} from "./transport.js";
export type { Method, Operation, OperationRequest, ResponseKind, Transport } from "./operation.js";
export type { UploadProgress } from "./progress.js";
export type {
  ContinuityLost,
  ContinuityLostEvent,
  OriginWithdrawn,
  OriginWithdrawnEvent,
  Envelope,
  EventsOptions,
  EventSubscription,
  HubEvent,
  StreamEvent,
} from "./stream.js";
export { bindOperations, operationTable, type Operations } from "./generated/operations.js";
export { errorCodes, type ErrorCode, type ErrorCodeEntry } from "./generated/errors.js";
export {
  eventClasses,
  eventTypes,
  eventTypesVersion,
  type EventClass,
  type EventPayloads,
  type EventType,
} from "./generated/events.js";
export {
  authorizationActions,
  authorizationRelations,
  authorizationTypes,
  authorizationVersion,
  objectTypes,
  subjectTypes,
  type AuthorizationAction,
  type AuthorizationRelation,
  type AuthorizationType,
} from "./generated/vocabulary.js";
import type {
  components as CoreComponents,
  operations as CoreOperations,
  paths as CorePaths,
} from "./generated/core-api.js";

/** Every schema the document names, under `schemas`, keyed by name. */
export type components = CoreComponents;
/** Every operation the document declares, keyed by operationId: its parameters, body and responses. */
export type operations = CoreOperations;
/** Every route the document declares, keyed by path and then by method. */
export type paths = CorePaths;
