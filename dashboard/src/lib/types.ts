export type JobStatus =
  | "pending"
  | "in-flight"
  | "retrying"
  | "succeeded"
  | "failed";

export type FailureKind = "retryable" | "terminal" | "exhausted";

export interface JobState {
  id: number;
  priority: number;
  status: JobStatus;
  attempts: number;
  url: string;
  createdAt: string;
  updatedAt: string;
  lastError?: string;
  lastStatusCode?: number;
  completedAt?: string;
}

export interface DispatchEvent {
  seq: number;
  jobId: number;
  at: string;
  kind: FailureKind;
  attempt: number;
  statusCode?: number;
  message: string;
  retryInMs?: number;
}

export interface QueueSample {
  at: string;
  depth: number;
  busy: number;
}

export interface Stats {
  queue: {
    depth: number;
    history: QueueSample[] | null;
  };
  workers: {
    total: number;
    busy: number;
    idle: number;
  };
  jobs: JobState[] | null;
  events: DispatchEvent[] | null;
  totals: Record<string, number>;
}

export const PRIORITY_LABELS = ["High", "Normal", "Low"] as const;

export function priorityLabel(p: number): string {
  return PRIORITY_LABELS[p] ?? `P${p}`;
}
