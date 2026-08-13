import { ListChecks } from "lucide-react";
import { Panel, Tag } from "./ui";
import { PriorityBadge, StatusBadge } from "./StatusBadge";
import type { JobState } from "@/lib/types";
import { formatAge, formatTime } from "@/lib/utils";

export function JobsTable({ jobs, now }: { jobs: JobState[]; now: number }) {
  return (
    <Panel
      icon={<ListChecks size={16} />}
      title="Jobs"
      subtitle="Most recent first"
      actions={<Tag>{jobs.length}</Tag>}
      flush
      style={{ height: 520 }}
    >
      {jobs.length === 0 ? (
        <div className="panel-empty">
          No jobs yet — submit one to watch it flow through the system.
        </div>
      ) : (
        <table className="table">
          <thead>
            <tr>
              <th>ID</th>
              <th>Priority</th>
              <th>Status</th>
              <th>Attempts</th>
              <th>Submitted</th>
              <th>Detail</th>
            </tr>
          </thead>
          <tbody>
            {jobs.map((j) => (
              <tr key={j.id}>
                <td className="col-id">#{j.id}</td>
                <td>
                  <PriorityBadge priority={j.priority} />
                </td>
                <td>
                  <StatusBadge status={j.status} />
                </td>
                <td>
                  {j.attempts > 0 ? (
                    <Tag tone={j.attempts > 1 ? "warn" : "neutral"} mono>
                      {j.attempts}
                    </Tag>
                  ) : (
                    <span style={{ color: "var(--text-tertiary)" }}>—</span>
                  )}
                </td>
                <td className="tnum" title={formatTime(j.createdAt)}>
                  {formatAge(j.createdAt, now)}
                </td>
                <td className="col-detail">
                  {j.lastError ? (
                    <span className="detail-error">{j.lastError}</span>
                  ) : (
                    <span className="detail-url">{j.url}</span>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </Panel>
  );
}
