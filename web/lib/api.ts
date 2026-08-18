import type {
  ApiErrorBody,
  Execution,
  ExecutionDetailResponse,
  ExecutionListResponse,
  Graph,
  Item,
  NodeDescriptor,
  NodeTestResponse,
  Status,
  User,
  WorkflowDetailResponse,
  WorkflowSummary,
  WorkflowVersion,
} from "./types";

export const API_BASE = "/api/v1";

/** An error carrying the API's own code so callers can branch on it. */
export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
    readonly details?: Record<string, unknown>,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    ...init,
    credentials: "include",
    headers: {
      ...(init?.body ? { "Content-Type": "application/json" } : {}),
      ...init?.headers,
    },
  });

  if (res.status === 204) return undefined as T;

  const text = await res.text();
  const body = text ? (JSON.parse(text) as unknown) : null;

  if (!res.ok) {
    const err = (body as ApiErrorBody | null)?.error;
    throw new ApiError(
      res.status,
      err?.code ?? "unknown",
      err?.message ?? res.statusText,
      err?.details,
    );
  }
  return body as T;
}

// --- auth -------------------------------------------------------------------

export const auth = {
  login: (email: string, password: string) =>
    request<{ user: User }>("/auth/login", {
      method: "POST",
      body: JSON.stringify({ email, password }),
    }),
  logout: () => request<void>("/auth/logout", { method: "POST" }),
  me: () => request<{ user: User }>("/auth/me"),
};

// --- node types -------------------------------------------------------------

export const nodeTypes = {
  list: () => request<{ nodeTypes: NodeDescriptor[] }>("/node-types"),
  test: (type: string, params: Record<string, unknown>, inputItems: Item[]) =>
    request<NodeTestResponse>(`/nodes/${encodeURIComponent(type)}/test`, {
      method: "POST",
      body: JSON.stringify({ params, inputItems }),
    }),
};

// --- workflows --------------------------------------------------------------

export interface WorkflowListQuery {
  q?: string;
  status?: "all" | "active" | "inactive";
  sort?: "updated" | "name" | "created";
}

export const workflows = {
  list: (query: WorkflowListQuery = {}) => {
    const qs = new URLSearchParams();
    if (query.q) qs.set("q", query.q);
    if (query.status && query.status !== "all") qs.set("status", query.status);
    if (query.sort) qs.set("sort", query.sort);
    const suffix = qs.size ? `?${qs}` : "";
    return request<{ workflows: WorkflowSummary[] }>(`/workflows${suffix}`);
  },
  create: (name: string) =>
    request<WorkflowDetailResponse>("/workflows", {
      method: "POST",
      body: JSON.stringify({ name }),
    }),
  get: (id: string) => request<WorkflowDetailResponse>(`/workflows/${id}`),
  patch: (id: string, patch: { name?: string; active?: boolean }) =>
    request<WorkflowDetailResponse>(`/workflows/${id}`, {
      method: "PATCH",
      body: JSON.stringify(patch),
    }),
  remove: (id: string) => request<void>(`/workflows/${id}`, { method: "DELETE" }),
  saveVersion: (id: string, graph: Graph) =>
    request<{ version: WorkflowVersion }>(`/workflows/${id}/versions`, {
      method: "POST",
      body: JSON.stringify({ graph }),
    }),
  run: (id: string, data?: unknown) =>
    request<{ execution: Execution }>(`/workflows/${id}/run`, {
      method: "POST",
      body: JSON.stringify({ data: data ?? null }),
    }),
};

// --- executions -------------------------------------------------------------

export interface ExecutionListQuery {
  workflowId?: string;
  status?: Status | "all";
  limit?: number;
  cursor?: string;
}

export const executions = {
  list: (query: ExecutionListQuery = {}) => {
    const qs = new URLSearchParams();
    if (query.workflowId) qs.set("workflowId", query.workflowId);
    if (query.status && query.status !== "all") qs.set("status", query.status);
    if (query.limit) qs.set("limit", String(query.limit));
    if (query.cursor) qs.set("cursor", query.cursor);
    const suffix = qs.size ? `?${qs}` : "";
    return request<ExecutionListResponse>(`/executions${suffix}`);
  },
  get: (id: string) => request<ExecutionDetailResponse>(`/executions/${id}`),
  cancel: (id: string) =>
    request<{ execution: Execution }>(`/executions/${id}/cancel`, { method: "POST" }),
  retry: (id: string) =>
    request<{ execution: Execution }>(`/executions/${id}/retry`, { method: "POST" }),
  streamUrl: (id: string) => `${API_BASE}/executions/${id}/stream`,
};
