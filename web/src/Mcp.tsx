import { useEffect, useState } from "react";
import { api, type McpStatus } from "./api";
import { ErrorText } from "./App";

export default function Mcp() {
  const [status, setStatus] = useState<McpStatus>();
  const [error, setError] = useState<unknown>();
  const [copyError, setCopyError] = useState<unknown>();
  const [checked, setChecked] = useState<string>();
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
        if (active) {
          setStatus(undefined);
          setError(reason);
        }
      } finally {
        if (active) setChecked(new Date().toISOString());
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

  return (
    <div className="stack calendar-page">
      <div className="page-heading">
        <div>
          <h1>MCP</h1>
          <p>AI clients launch this command over stdio; it shares this app's database and acts as Martin.</p>
        </div>
      </div>
      <section className="panel calendar-form-panel">
        <h2>Connect an AI client</h2>
        <pre><code>{config}</code></pre>
        <button type="button" onClick={copyConfig} disabled={!status}>
          {copied ? "Copied" : "Copy"}
        </button>
        <ErrorText error={copyError} />
      </section>
      <section className="panel calendar-form-panel">
        <div className="page-heading">
          <h2>Self-test</h2>
          <button type="button" onClick={() => setRefresh((value) => value + 1)}>
            Check again
          </button>
        </div>
        <p role="status" aria-live="polite">
          {status
            ? status.ok
              ? `Healthy · ${status.latency_ms} ms · ${status.tools.length} tools`
              : `Unhealthy · ${status.error}`
            : error
              ? "Unable to check MCP status"
              : "Checking…"}
        </p>
        <p>Last checked: {checked ? new Date(checked).toLocaleTimeString() : "Never"}</p>
        <ErrorText error={error} />
        <ul>
          {status?.tools.map((tool) => (
            <li key={tool.name}><code>{tool.name}</code> — {tool.description}</li>
          ))}
        </ul>
      </section>
    </div>
  );
}
