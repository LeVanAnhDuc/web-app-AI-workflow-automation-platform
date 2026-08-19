// Mirror of the Go API's JSON shapes. Kept hand-written and small on purpose:
// it is the one place the frontend and backend contracts meet.

export type Status =
  | "queued"
  | "running"
  | "succeeded"
  | "failed"
  | "skipped"
  | "cancelled";

export type TriggerType = "manual" | "webhook" | "schedule";

export interface Item {
  json: Record<string, unknown>;
}

export interface NodeError {
  code: string;
  message: string;
  status?: number;
  node_id?: string;
  node_name?: string;
  attempts?: number;
  details?: Record<string, unknown>;
}

export interface Position {
  x: number;
  y: number;
}

export interface NodeSettings {
  retryOnFail: boolean;
  maxTries: number;
  waitBetweenTriesMs: number;
  continueOnFail: boolean;
  timeoutMs: number;
}

export const defaultNodeSettings: NodeSettings = {
  retryOnFail: false,
  maxTries: 3,
  waitBetweenTriesMs: 1000,
  continueOnFail: false,
  timeoutMs: 60000,
};

export interface GraphNode {
  id: string;
  type: string;
  name: string;
  position: Position;
  params: Record<string, unknown>;
  credentialId?: string | null;
  settings: NodeSettings;
  disabled?: boolean;
}

export interface GraphEdge {
  id: string;
  source: string;
  sourceHandle: string;
  target: string;
  targetHandle: string;
}

export interface Graph {
  nodes: GraphNode[];
  edges: GraphEdge[];
}

// --- node descriptors -------------------------------------------------------

export type ParamType =
  | "string"
  | "number"
  | "boolean"
  | "select"
  | "json"
  | "code"
  | "keyValue"
  | "notice"
  | "filter";

export interface ParamOption {
  Label: string;
  Value: string;
}

export interface ShowWhen {
  Param: string;
  Equals: unknown[];
}

export interface ParamSpec {
  Name: string;
  Label: string;
  Type: ParamType;
  Default?: unknown;
  Required?: boolean;
  Options?: ParamOption[] | null;
  Placeholder?: string;
  Description?: string;
  SupportsExpression?: boolean;
  ShowWhen?: ShowWhen | null;
}

export interface Handle {
  Name: string;
  Label: string;
}

export interface NodeDescriptor {
  Type: string;
  Name: string;
  Category: string;
  Description: string;
  Icon: string;
  Mode: "perItem" | "once";
  Inputs: Handle[] | null;
  Outputs: Handle[] | null;
  Params: ParamSpec[] | null;
  Credential: string;
  IsTrigger: boolean;
}

// --- resources --------------------------------------------------------------

export interface User {
  id: string;
  workspaceId: string;
  email: string;
  role: string;
  createdAt: string;
}

export interface Workflow {
  id: string;
  workspaceId: string;
  name: string;
  active: boolean;
  activeVersionId?: string;
  createdAt: string;
  updatedAt: string;
}

export interface WorkflowVersion {
  id: string;
  workflowId: string;
  version: number;
  graph: Graph;
  createdAt: string;
}

export interface LastRun {
  id: string;
  status: Status;
  at: string;
}

export interface WorkflowSummary {
  id: string;
  name: string;
  active: boolean;
  nodeCount: number;
  triggerType: TriggerType;
  triggerDetail?: string;
  updatedAt: string;
  lastRun?: LastRun;
  successRate7d?: number;
  executionCount: number;
}

export interface Execution {
  id: string;
  workspaceId: string;
  workflowId: string;
  workflowName?: string;
  workflowVersionId: string;
  version?: number;
  status: Status;
  triggerType: TriggerType;
  triggerData?: Item[];
  error?: NodeError;
  resumeFromNode?: string;
  cancelRequested: boolean;
  createdAt: string;
  startedAt?: string;
  finishedAt?: string;
  durationMs?: number;
}

export interface NodeExecution {
  id: string;
  executionId: string;
  nodeId: string;
  nodeName: string;
  nodeType: string;
  status: Status;
  attempt: number;
  input?: Item[];
  output?: Record<string, Item[]>;
  error?: NodeError;
  startedAt?: string;
  finishedAt?: string;
  durationMs?: number;
}

// --- responses --------------------------------------------------------------

export interface ApiErrorBody {
  error: { code: string; message: string; details?: Record<string, unknown> };
}

export interface WorkflowDetailResponse {
  workflow: Workflow;
  graph: Graph;
  version: number;
  webhookUrl?: string;
}

export interface ExecutionDetailResponse {
  execution: Execution;
  nodeExecutions: NodeExecution[];
  graph: Graph;
}

export interface ExecutionListResponse {
  executions: Execution[];
  nextCursor?: string;
}

export interface NodeTestResponse {
  outputs?: Record<string, Item[]>;
  error?: NodeError;
}

// SSE payloads emitted by GET /executions/{id}/stream
export interface ExecutionStreamEvent {
  type: "execution" | "node" | "done";
  execution?: Execution;
  node?: NodeExecution;
}

// --- credentials ------------------------------------------------------------
// Mirrors internal/domain/credential.go. A credential's *shape* comes from its
// type descriptor, exactly as a node's form comes from its node descriptor, so
// adding an authentication scheme on the Go side needs no change here.

export type AuthKind = "none" | "apiKey" | "basic" | "bearer" | "oauth2";

/** `notice` carries prose and no value; `hidden` carries a value the user never
 *  sees but which still has to be submitted. */
export type CredentialFieldType = "string" | "password" | "select" | "notice" | "hidden";

export interface CredentialFieldOption {
  label: string;
  value: string;
}

export interface CredentialField {
  name: string;
  label: string;
  type: CredentialFieldType;
  required?: boolean;
  default?: string;
  placeholder?: string;
  description?: string;
  /** Never returned by the API once stored: the form shows "unchanged". */
  secret?: boolean;
  options?: CredentialFieldOption[] | null;
}

export interface CredentialOAuth2Config {
  authorizeUrl: string;
  tokenUrl: string;
  scopes?: string[] | null;
}

export interface CredentialType {
  type: string;
  name: string;
  icon: string;
  description?: string;
  auth: AuthKind;
  fields: CredentialField[];
  oauth2?: CredentialOAuth2Config | null;
  /** Empty means "cannot be tested" — the UI says so rather than offering a
   *  button that does nothing. */
  testUrl?: string;
}

/** What the API returns for a stored credential: everything except the values
 *  worth protecting. */
export interface CredentialSummary {
  id: string;
  workspaceId: string;
  type: string;
  name: string;
  createdAt: string;
  updatedAt: string;
  /** Which fields hold a value — the basis of "unchanged" and "Incomplete". */
  setFields: string[];
  /** OAuth only: whether the authorisation dance completed. */
  connected: boolean;
  /** OAuth access-token expiry. */
  expiresAt?: string;
  /** How many nodes reference it, which is what makes a delete confirmation
   *  honest. */
  usedByCount: number;
}

// --- credential responses ---------------------------------------------------

export interface CredentialTypeListResponse {
  credentialTypes: CredentialType[];
  /** The one string the user must register with the provider verbatim. */
  redirectUri: string;
}

export interface CredentialListResponse {
  credentials: CredentialSummary[];
}

export interface CredentialResponse {
  credential: CredentialSummary;
}

export interface CredentialTestResult {
  ok: boolean;
  status?: number;
  message?: string;
}

export interface OAuthStartResponse {
  url: string;
  state: string;
}
