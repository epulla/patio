import { useEffect, useState } from "react";
import { api, type McpStatus } from "./api";
import { ErrorText } from "./App";

export default function Mcp() {
  const [status, setStatus] = useState<McpStatus>();
  const [error, setError] = useState<unknown>();
  const [copyError, setCopyError] = useState<unknown>();
  const [checked, setChecked] = useState<Date>();
  const [refresh, setRefresh] = useState(0);
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    let active = true;
    const check = async () => {
      try {
        const result = await api.get<McpStatus>("/api/mcp");
        if (active) {
          setStatus(result);
          setError(undefined);
        }
      } catch (reason) {
        if (active) setError(reason);
      } finally {
        if (active) setChecked(new Date());
      }
    };
    void check();
    const timer = window.setInterval(check, 30000);
    return () => {
      active = false;
      window.clearInterval(timer);
    };
  }, [refresh]);

  const config = status
    ? JSON.stringify(
        { mcpServers: { "my-tickets": { command: status.command, args: status.args } } },
        null,
        2,
      )
    : "Loading…";

  async function copyConfig() {
    try {
      await navigator.clipboard.writeText(config);
      setCopyError(undefined);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1500);
    } catch (reason) {
      setCopyError(reason);
    }
  }

  const state = error || status?.ok === false ? "down" : status ? "up" : "pending";
  const label = { up: "Healthy", down: "Unhealthy", pending: "Checking…" }[state];
  const detail = error
    ? "Could not reach the app backend."
    : status?.ok
      ? `MCP server responded in ${status.latency_ms} ms with ${status.tools.length} tools. Everything is fine.`
      : status?.error;

  return (
    <div className="stack mcp-page">
      <div className="page-heading">
        <div>
          <h1>MCP</h1>
          <p className="muted">
            AI clients launch this app as an MCP server over stdio. It shares this app's database and acts as
            Martin.
          </p>
        </div>
      </div>

      <section className={`panel mcp-status ${state}`}>
        <span className="status-dot" aria-hidden="true" />
        <div className="mcp-status-text" role="status" aria-live="polite">
          <strong>{label}</strong>
          {detail && <span className="muted">{detail}</span>}
          <small>Last checked {checked ? checked.toLocaleTimeString() : "never"} · rechecks every 30 s</small>
        </div>
        <button type="button" onClick={() => setRefresh((value) => value + 1)}>
          Check again
        </button>
      </section>
      <ErrorText error={error} />

      <section className="panel">
        <div className="mcp-section-head">
          <div>
            <h2>Connect an AI client</h2>
            <p className="muted">Add this to your client's MCP config (Claude Desktop, Cursor, Claude Code…).</p>
          </div>
          <button type="button" onClick={copyConfig} disabled={!status}>
            {copied ? "Copied" : "Copy"}
          </button>
        </div>
        <pre className="mcp-config">
          <code>{config}</code>
        </pre>
        <ErrorText error={copyError} />
      </section>

      {status?.ok && (
        <section className="panel">
          <h2>
            Tools <small>{status.tools.length}</small>
          </h2>
          <dl className="mcp-tools">
            {status.tools.map((tool) => (
              <div key={tool.name}>
                <dt>
                  <code>{tool.name}</code>
                </dt>
                <dd>{tool.description}</dd>
              </div>
            ))}
          </dl>
        </section>
      )}
    </div>
  );
}
