import { useState } from "react";
import { CheckCircle2, Loader2, Send, XCircle, Zap } from "lucide-react";
import { Field, Panel } from "./ui";
import { submitJob } from "@/lib/api";

/**
 * Presets target the local flaky receiver so a demo can trigger each retry
 * classification without hand-editing the URL.
 */
const PRESETS = [
  { label: "Succeeds", url: "http://localhost:9090/status/200", tone: "ok" },
  { label: "Retries, then OK", url: "http://localhost:9090/flaky/2", tone: "warn" },
  { label: "Always 500", url: "http://localhost:9090/status/500", tone: "danger" },
  { label: "404 terminal", url: "http://localhost:9090/status/404", tone: "danger" },
] as const;

export function SubmitForm({ onSubmitted }: { onSubmitted: () => void }) {
  const [url, setUrl] = useState<string>(PRESETS[1].url);
  const [priority, setPriority] = useState(0);
  const [payload, setPayload] = useState('{"hello":"world"}');
  const [count, setCount] = useState(1);
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<{ ok: boolean; message: string } | null>(
    null,
  );

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setResult(null);

    const ids: number[] = [];
    let error: string | null = null;

    for (let i = 0; i < count; i++) {
      // The dedup cache rejects repeated idempotency keys, so each submission
      // needs a distinct one.
      const idemKey = `ui-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;

      // Tag the payload so the /flaky/{n} receiver counts attempts per job
      // rather than globally. Dispatch reuses the payload across retries, so
      // this stays stable for the job's whole retry sequence.
      let tagged = payload;
      try {
        const parsed = JSON.parse(payload);
        if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
          tagged = JSON.stringify({ ...parsed, demoJobId: idemKey });
        }
      } catch {
        // Leave as-is; submitJob reports the parse error.
      }

      const res = await submitJob({ url, priority, payload: tagged, idemKey });

      if (!res.ok) {
        error = res.error ?? "Submission failed";
        break;
      }

      if (res.id !== undefined) ids.push(res.id);
    }

    setBusy(false);
    setResult(
      error
        ? { ok: false, message: error }
        : {
            ok: true,
            message:
              ids.length === 1
                ? `Job #${ids[0]} submitted`
                : `${ids.length} jobs submitted (#${ids[0]}–#${ids[ids.length - 1]})`,
          },
    );

    onSubmitted();
  }

  return (
    <Panel
      icon={<Send size={16} />}
      title="Submit test job"
      subtitle="Fire a real job and watch it flow through"
    >
      <div className="presets">
        {PRESETS.map((p) => (
          <button
            key={p.url}
            type="button"
            className={`preset${url === p.url ? " active" : ""}`}
            onClick={() => setUrl(p.url)}
          >
            <span className={`preset-dot ${p.tone}`} />
            {p.label}
          </button>
        ))}
      </div>

      <form onSubmit={handleSubmit}>
        <Field label="Target URL">
          <input
            className="input mono"
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            placeholder="http://localhost:9090/hook"
          />
        </Field>

        <div className="field-row">
          <Field label="Priority">
            <select
              className="select"
              value={priority}
              onChange={(e) => setPriority(Number(e.target.value))}
            >
              <option value={0}>High</option>
              <option value={1}>Normal</option>
              <option value={2}>Low</option>
            </select>
          </Field>

          <Field label="How many">
            <input
              type="number"
              min={1}
              max={50}
              className="input tnum"
              value={count}
              onChange={(e) =>
                setCount(Math.min(50, Math.max(1, Number(e.target.value))))
              }
            />
          </Field>
        </div>

        <Field label="Payload (JSON)">
          <textarea
            className="textarea mono"
            rows={3}
            value={payload}
            onChange={(e) => setPayload(e.target.value)}
            spellCheck={false}
          />
        </Field>

        <div className="form-actions">
          <button type="submit" className="btn" disabled={busy}>
            {busy ? (
              <>
                <Loader2 size={15} className="spin" />
                Submitting…
              </>
            ) : (
              <>
                <Zap size={15} />
                {count > 1 ? `Submit ${count} jobs` : "Submit job"}
              </>
            )}
          </button>

          {result ? (
            <span className={`form-result ${result.ok ? "ok" : "err"}`}>
              {result.ok ? <CheckCircle2 size={14} /> : <XCircle size={14} />}
              {result.message}
            </span>
          ) : null}
        </div>
      </form>
    </Panel>
  );
}
