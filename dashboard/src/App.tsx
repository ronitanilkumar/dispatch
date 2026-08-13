import { useCallback, useEffect, useRef, useState } from "react";
import {
  Boxes,
  CheckCircle2,
  Layers,
  Moon,
  RefreshCw,
  Sun,
  Wifi,
  WifiOff,
  XCircle,
  Zap,
} from "lucide-react";
import { QueueChart } from "@/components/QueueChart";
import { WorkerGrid } from "@/components/WorkerGrid";
import { JobsTable } from "@/components/JobsTable";
import { FailureFeed } from "@/components/FailureFeed";
import { SubmitForm } from "@/components/SubmitForm";
import { Stat } from "@/components/ui";
import { fetchStats } from "@/lib/api";
import type { Stats } from "@/lib/types";

const POLL_MS = 400;
const THEME_KEY = "dispatch-theme";

export default function App() {
  const [stats, setStats] = useState<Stats | null>(null);
  const [connected, setConnected] = useState(true);
  const [now, setNow] = useState(Date.now());
  const [theme, setTheme] = useState<"dark" | "light">(
    () => (localStorage.getItem(THEME_KEY) as "dark" | "light") ?? "dark",
  );

  useEffect(() => {
    document.documentElement.setAttribute("data-theme", theme);
    localStorage.setItem(THEME_KEY, theme);
  }, [theme]);

  // Held in a ref so the polling effect never needs to re-subscribe, and so a
  // slow response cannot stack up overlapping requests.
  const inFlight = useRef(false);

  const poll = useCallback(async () => {
    if (inFlight.current) return;
    inFlight.current = true;

    try {
      setStats(await fetchStats());
      setConnected(true);
    } catch {
      setConnected(false);
    } finally {
      inFlight.current = false;
    }
  }, []);

  useEffect(() => {
    poll();
    const id = setInterval(poll, POLL_MS);
    return () => clearInterval(id);
  }, [poll]);

  // Drives relative timestamps without re-fetching.
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, []);

  const jobs = stats?.jobs ?? [];
  const events = stats?.events ?? [];
  const history = stats?.queue.history ?? [];
  const workers = stats?.workers ?? { total: 0, busy: 0, idle: 0 };
  const totals = stats?.totals ?? {};

  return (
    <div className="app">
      <header className="topbar">
        <div className="topbar-left">
          <span className="topbar-mark">
            <Boxes size={18} />
          </span>
          <div style={{ minWidth: 0 }}>
            <div className="topbar-title">Dispatch</div>
            <div className="topbar-sub">Concurrent webhook delivery engine</div>
          </div>
        </div>

        <div className="topbar-right">
          {connected ? (
            <span className="topbar-pill connected">
              <Wifi size={14} />
              Connected
            </span>
          ) : (
            <span className="topbar-disconnected">
              <WifiOff size={14} />
              Backend unreachable
            </span>
          )}

          <span className="topbar-pill">
            <RefreshCw size={14} />
            {POLL_MS}ms
          </span>

          <span className="topbar-sep" />

          <button
            className="icon-btn"
            onClick={() => setTheme(theme === "dark" ? "light" : "dark")}
            title={`Switch to ${theme === "dark" ? "light" : "dark"} theme`}
            aria-label="Toggle theme"
          >
            {theme === "dark" ? <Sun size={18} /> : <Moon size={18} />}
          </button>
        </div>
      </header>

      <main className="content">
        <div className="grid grid-stats">
          <Stat
            icon={<Layers size={18} />}
            label="Queue depth"
            value={stats?.queue.depth ?? 0}
            tone="blue"
          />
          <Stat
            icon={<Zap size={18} />}
            label="Busy workers"
            value={`${workers.busy}/${workers.total}`}
            tone={workers.busy > 0 ? "ok" : undefined}
          />
          <Stat
            icon={<RefreshCw size={18} />}
            label="Retrying"
            value={totals["retrying"] ?? 0}
            tone="warn"
          />
          <Stat
            icon={<CheckCircle2 size={18} />}
            label="Succeeded"
            value={totals["succeeded"] ?? 0}
            tone="ok"
          />
          <Stat
            icon={<XCircle size={18} />}
            label="Failed"
            value={totals["failed"] ?? 0}
            tone="danger"
          />
        </div>

        <div className="grid grid-main">
          <QueueChart
            history={history}
            workers={workers.total}
            theme={theme}
          />
          <WorkerGrid total={workers.total} busy={workers.busy} />
        </div>

        <div className="grid grid-work">
          <SubmitForm onSubmitted={poll} />
          <JobsTable jobs={jobs} now={now} />
        </div>

        <FailureFeed events={events} />
      </main>
    </div>
  );
}
