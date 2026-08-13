import type { ReactNode } from "react";

export function Panel({
  icon,
  title,
  subtitle,
  actions,
  flush,
  bodyClassName,
  className,
  style,
  children,
}: {
  icon?: ReactNode;
  title: string;
  subtitle?: string;
  actions?: ReactNode;
  flush?: boolean;
  bodyClassName?: string;
  className?: string;
  style?: React.CSSProperties;
  children: ReactNode;
}) {
  return (
    <section className={`panel${className ? ` ${className}` : ""}`} style={style}>
      <header className="panel-header">
        {icon ? <span className="panel-icon">{icon}</span> : null}
        <div style={{ minWidth: 0, flex: 1 }}>
          <div className="panel-title">{title}</div>
          {subtitle ? <div className="panel-sub">{subtitle}</div> : null}
        </div>
        {actions}
      </header>
      <div
        className={`panel-body${flush ? " flush scroll" : ""}${
          bodyClassName ? ` ${bodyClassName}` : ""
        }`}
      >
        {children}
      </div>
    </section>
  );
}

export type Tone = "neutral" | "blue" | "ok" | "warn" | "danger";

export function Tag({
  tone = "neutral",
  mono,
  children,
}: {
  tone?: Tone;
  mono?: boolean;
  children: ReactNode;
}) {
  const cls = ["tag", tone !== "neutral" ? tone : "", mono ? "mono" : ""]
    .filter(Boolean)
    .join(" ");

  return <span className={cls}>{children}</span>;
}

export function Stat({
  icon,
  label,
  value,
  tone,
}: {
  icon: ReactNode;
  label: string;
  value: ReactNode;
  tone?: "ok" | "warn" | "danger" | "blue";
}) {
  return (
    <div className="stat">
      <span className={`stat-icon${tone ? ` ${tone}` : ""}`}>{icon}</span>
      <div style={{ minWidth: 0 }}>
        <div className="stat-label">{label}</div>
        <div className="stat-value">{value}</div>
      </div>
    </div>
  );
}

export function Field({
  label,
  children,
}: {
  label: string;
  children: ReactNode;
}) {
  return (
    <label className="field">
      <span className="field-label">{label}</span>
      {children}
    </label>
  );
}
