import type { Stats } from "./types";

export interface SubmitJobInput {
  url: string;
  priority: number;
  payload: string;
  idemKey: string;
}

export async function fetchStats(signal?: AbortSignal): Promise<Stats> {
  const res = await fetch("/api/stats", { signal });

  if (!res.ok) {
    throw new Error(`stats request failed: ${res.status}`);
  }

  return res.json();
}

export interface SubmitResult {
  ok: boolean;
  id?: number;
  error?: string;
}

export async function submitJob(input: SubmitJobInput): Promise<SubmitResult> {
  let payload: unknown;

  try {
    payload = JSON.parse(input.payload);
  } catch {
    return { ok: false, error: "Payload must be valid JSON" };
  }

  const res = await fetch("/jobs", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      payload,
      priority: input.priority,
      url: input.url,
      idemKey: input.idemKey,
    }),
  });

  if (!res.ok) {
    // The Go API returns plain-text errors via http.Error.
    const text = (await res.text()).trim();
    return { ok: false, error: text || `Request failed (${res.status})` };
  }

  const body = await res.json();
  return { ok: true, id: body.id };
}
