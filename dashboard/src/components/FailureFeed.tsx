import { AlertTriangle, Ban, Clock3, RotateCw, ShieldAlert } from "lucide-react";
import { Panel, Tag } from "./ui";
import type { DispatchEvent, FailureKind } from "@/lib/types";
import { formatDuration, formatTime } from "@/lib/utils";

/**
 * Mirrors delivery.Client's classification:
 *  - retryable: 429/5xx or transport error -> back off and retry
 *  - terminal:  3xx/4xx                    -> give up immediately
 *  - exhausted: retryable but maxAttempts reached
 */
function kindMeta(kind: FailureKind) {
  switch (kind) {
    case "retryable":
      return { icon: <RotateCw size={12} />, tone: "warn" as const, label: "Retryable" };
    case "terminal":
      return { icon: <Ban size={12} />, tone: "danger" as const, label: "Terminal" };
    case "exhausted":
      return { icon: <ShieldAlert size={12} />, tone: "danger" as const, label: "Exhausted" };
  }
}

export function FailureFeed({ events }: { events: DispatchEvent[] }) {
  const retryable = events.filter((e) => e.kind === "retryable").length;
  const terminal = events.length - retryable;

  return (
    <Panel
      icon={<AlertTriangle size={16} />}
      title="Retry & failure feed"
      subtitle="Live delivery classification"
      actions={
        <div style={{ display: "flex", gap: 6 }}>
          <Tag tone="warn">{retryable} retryable</Tag>
          <Tag tone="danger">{terminal} terminal</Tag>
        </div>
      }
      flush
      style={{ height: 380 }}
    >
      {events.length === 0 ? (
        <div className="panel-empty">
          No failures yet — target a URL that returns 500 to see retries.
        </div>
      ) : (
        <div>
          {events.map((e) => {
            const meta = kindMeta(e.kind);
            return (
              <div key={e.seq} className="feed-row">
                <div className="feed-head">
                  <Tag tone={meta.tone}>
                    {meta.icon}
                    {meta.label}
                  </Tag>
                  <span className="feed-job">#{e.jobId}</span>
                  <span className="feed-attempt">attempt {e.attempt}</span>
                  {e.statusCode ? (
                    <Tag mono>HTTP {e.statusCode}</Tag>
                  ) : null}
                  <span className="feed-time">{formatTime(e.at)}</span>
                </div>

                <div className="feed-msg">{e.message}</div>

                {e.retryInMs ? (
                  <div className="feed-backoff">
                    <Clock3 size={12} />
                    backing off {formatDuration(e.retryInMs)} before next attempt
                  </div>
                ) : null}
              </div>
            );
          })}
        </div>
      )}
    </Panel>
  );
}
