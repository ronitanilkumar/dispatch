import { Cpu } from "lucide-react";
import { Panel, Tag } from "./ui";

export function WorkerGrid({ total, busy }: { total: number; busy: number }) {
  const idle = Math.max(total - busy, 0);
  const pct = total > 0 ? Math.round((busy / total) * 100) : 0;

  return (
    <Panel
      icon={<Cpu size={16} />}
      title="Worker pool"
      subtitle={`${busy} busy · ${idle} idle`}
      actions={<Tag tone={busy > 0 ? "ok" : "neutral"}>{pct}%</Tag>}
      bodyClassName="worker-body"
    >
      <div className="worker-meter">
        <div className="worker-meter-fill" style={{ width: `${pct}%` }} />
      </div>

      <div className="worker-slots">
        {Array.from({ length: total }, (_, i) => (
          <div
            key={i}
            className={`worker-slot${i < busy ? " busy" : ""}`}
            title={i < busy ? "busy" : "idle"}
          />
        ))}
      </div>

      <div className="legend">
        <span className="legend-item">
          <span className="legend-swatch busy" />
          Busy
        </span>
        <span className="legend-item">
          <span className="legend-swatch" />
          Idle
        </span>
      </div>
    </Panel>
  );
}
