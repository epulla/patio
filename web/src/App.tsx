import { useEffect, useState } from "react";

export default function App() {
  const [healthy, setHealthy] = useState<boolean | null>(null);
  useEffect(() => { fetch("/api/health").then((r) => r.ok).then(setHealthy).catch(() => setHealthy(false)); }, []);
  return <main><h1>my-tickets</h1><p>API: {healthy === null ? "checking..." : healthy ? "healthy" : "unavailable"}</p></main>;
}
