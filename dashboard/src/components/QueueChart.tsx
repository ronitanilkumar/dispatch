import { useEffect, useState } from "react";
import {
  Area,
  AreaChart,
  CartesianGrid,
  Line,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import { Activity } from "lucide-react";
import { Panel, Tag } from "./ui";
import type { QueueSample } from "@/lib/types";
import { formatTime } from "@/lib/utils";

/**
 * Recharts needs real color values, not CSS variables, so the palette is read
 * off the document once per theme change rather than hardcoded.
 */
function usePalette(theme: string) {
  const [palette, setPalette] = useState({
    blue: "#74c0fc",
    ok: "#69db7c",
    border: "#26262e",
    surface: "#121215",
    text: "#e8e8ed",
    muted: "#46464f",
  });

  useEffect(() => {
    const s = getComputedStyle(document.documentElement);
    const read = (name: string, fallback: string) =>
      s.getPropertyValue(name).trim() || fallback;

    setPalette({
      blue: read("--accent-blue", "#74c0fc"),
      ok: read("--ok", "#69db7c"),
      border: read("--border-subtle", "#26262e"),
      surface: read("--surface", "#121215"),
      text: read("--text-primary", "#e8e8ed"),
      muted: read("--text-tertiary", "#46464f"),
    });
  }, [theme]);

  return palette;
}

export function QueueChart({
  history,
  workers,
  theme,
}: {
  history: QueueSample[];
  workers: number;
  theme: string;
}) {
  const p = usePalette(theme);

  const data = history.map((s) => ({
    t: formatTime(s.at),
    depth: s.depth,
    busy: s.busy,
  }));

  const peak = data.reduce((m, d) => Math.max(m, d.depth), 0);

  return (
    <Panel
      icon={<Activity size={16} />}
      title="Queue depth over time"
      subtitle={`Peak per 100ms window · pool size ${workers}`}
      actions={<Tag tone={peak > 0 ? "blue" : "neutral"}>peak {peak}</Tag>}
    >
      <div className="chart-wrap">
        {data.length === 0 ? (
          <div className="panel-empty">Waiting for samples…</div>
        ) : (
          <ResponsiveContainer width="100%" height="100%">
            <AreaChart
              data={data}
              margin={{ top: 6, right: 6, bottom: 0, left: 0 }}
            >
              <defs>
                <linearGradient id="depthFill" x1="0" y1="0" x2="0" y2="1">
                  <stop offset="0%" stopColor={p.blue} stopOpacity={0.28} />
                  <stop offset="100%" stopColor={p.blue} stopOpacity={0.01} />
                </linearGradient>
              </defs>
              <CartesianGrid
                stroke={p.border}
                strokeDasharray="2 4"
                vertical={false}
              />
              <XAxis
                dataKey="t"
                stroke={p.border}
                tickLine={false}
                minTickGap={56}
              />
              <YAxis
                allowDecimals={false}
                stroke={p.border}
                tickLine={false}
                width={32}
              />
              <Tooltip
                contentStyle={{
                  background: p.surface,
                  border: `1px solid ${p.border}`,
                  borderRadius: 10,
                  fontSize: 12,
                  fontFamily: "var(--font)",
                  color: p.text,
                  boxShadow: "0 8px 24px var(--shadow)",
                }}
                labelStyle={{ color: p.muted, fontSize: 11 }}
                cursor={{ stroke: p.border }}
              />
              <Area
                type="monotone"
                dataKey="depth"
                name="Queue depth"
                stroke={p.blue}
                strokeWidth={1.75}
                fill="url(#depthFill)"
                isAnimationActive={false}
                dot={false}
              />
              <Line
                type="monotone"
                dataKey="busy"
                name="Busy workers"
                stroke={p.ok}
                strokeWidth={1.25}
                strokeDasharray="3 3"
                dot={false}
                isAnimationActive={false}
              />
            </AreaChart>
          </ResponsiveContainer>
        )}
      </div>
    </Panel>
  );
}
