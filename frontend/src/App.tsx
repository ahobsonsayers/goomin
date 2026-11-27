import { useCallback, useEffect, useState } from "react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Separator } from "@/components/ui/separator";

type Status = {
  google: boolean;
  garmin: boolean;
  garminAs: string;
  lastSync: string;
  log: string[];
};

const emptyStatus: Status = {
  google: false,
  garmin: false,
  garminAs: "",
  lastSync: "",
  log: [],
};

async function call(path: string, body?: unknown): Promise<string> {
  const response = await fetch(path, {
    method: body === undefined ? "GET" : "POST",
    headers: body === undefined ? {} : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const text = await response.text();
  if (!response.ok) throw new Error(text || `request failed (${response.status})`);

  const parsed = JSON.parse(text) as { message?: string };

  return parsed.message ?? "";
}

function connected(state: boolean, label: string) {
  return <Badge variant={state ? "default" : "secondary"}>{state ? label : "not connected"}</Badge>;
}

function App() {
  const [status, setStatus] = useState<Status>(emptyStatus);
  const [error, setError] = useState("");
  const [note, setNote] = useState("");
  const [asking, setAsking] = useState(false);
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const [wantCode, setWantCode] = useState(false);

  const refresh = useCallback(async () => {
    try {
      const response = await fetch("/api/status");
      if (!response.ok) return;
      setStatus((await response.json()) as Status);
    } catch {
      // The server is restarting; the next poll recovers.
    }
  }, []);

  useEffect(() => {
    void refresh();
    const timer = setInterval(() => void refresh(), 10_000);

    return () => clearInterval(timer);
  }, [refresh]);

  async function run(action: () => Promise<string>) {
    setError("");
    setNote("");
    try {
      setNote(await action());
      await refresh();
    } catch (problem) {
      setError(problem instanceof Error ? problem.message : String(problem));
    }
  }

  async function connectGarmin() {
    await run(async () => {
      await call("/api/auth/garmin/start", {});
      setAsking(true);
      setWantCode(false);
      setPassword("");
      setCode("");

      return "";
    });
  }

  async function signIn(event: React.FormEvent) {
    event.preventDefault();
    await run(async () => {
      const response = await fetch("/api/auth/garmin", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ username, password }),
      });
      const body = (await response.json()) as { mfa?: boolean; message?: string };
      if (!response.ok) throw new Error(body.message ?? "Garmin refused the sign-in");
      if (body.mfa) {
        setWantCode(true);
        setPassword("");

        return "Garmin sent a code to the account's authenticator";
      }
      setAsking(false);

      return "Garmin connected";
    });
  }

  async function submitCode(event: React.FormEvent) {
    event.preventDefault();
    await run(async () => {
      const message = await call("/api/auth/garmin/mfa", { code });
      setAsking(false);

      return message;
    });
  }

  return (
    <main className="mx-auto flex w-full max-w-3xl flex-col gap-6 p-6">
      <header className="flex flex-col gap-1">
        <h1 className="text-2xl font-semibold tracking-tight">goomin</h1>
        <p className="text-muted-foreground text-sm">
          Google Health exercises, uploaded to Garmin Connect every 12 hours.
        </p>
      </header>

      {error ? (
        <p className="border-destructive/40 bg-destructive/10 text-destructive rounded-md border p-3 text-sm">
          {error}
        </p>
      ) : null}
      {note ? <p className="text-muted-foreground rounded-md border p-3 text-sm">{note}</p> : null}

      <div className="grid gap-4 sm:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center justify-between gap-2">
              Google Health {connected(status.google, "connected")}
            </CardTitle>
            <CardDescription>Source of the workouts.</CardDescription>
          </CardHeader>
          <CardContent>
            {status.google ? (
              <Button
                variant="outline"
                onClick={() => void run(() => call("/api/disconnect/google", {}))}
              >
                Disconnect
              </Button>
            ) : (
              <Button onClick={() => (window.location.href = "/api/auth/google")}>
                Connect Google
              </Button>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="flex items-center justify-between gap-2">
              Garmin Connect {connected(status.garmin, "connected")}
            </CardTitle>
            <CardDescription>{status.garminAs || "Destination of the workouts."}</CardDescription>
          </CardHeader>
          <CardContent>
            {status.garmin ? (
              <Button
                variant="outline"
                onClick={() => void run(() => call("/api/disconnect/garmin", {}))}
              >
                Disconnect
              </Button>
            ) : (
              <Button onClick={() => void connectGarmin()}>Connect Garmin</Button>
            )}
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center justify-between gap-2">
            Sync
            <Button onClick={() => void run(() => call("/api/sync", {}))}>Sync now</Button>
          </CardTitle>
          <CardDescription>
            Last sync: {status.lastSync ? new Date(status.lastSync).toLocaleString() : "never"}
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Separator className="mb-3" />
          <pre className="max-h-80 overflow-auto rounded-md bg-muted p-3 font-mono text-xs leading-relaxed">
            {status.log.length > 0 ? status.log.join("\n") : "Nothing logged yet."}
          </pre>
        </CardContent>
      </Card>

      {asking ? (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4">
          <Card className="w-full max-w-sm">
            <CardHeader>
              <CardTitle>Garmin sign-in</CardTitle>
              <CardDescription>
                Your password goes straight to Garmin. Only the token is kept here.
              </CardDescription>
            </CardHeader>
            <CardContent>
              {wantCode ? (
                <form className="flex flex-col gap-3" onSubmit={(e) => void submitCode(e)}>
                  <div className="flex flex-col gap-1.5">
                    <Label htmlFor="code">Code from your authenticator</Label>
                    <Input
                      id="code"
                      value={code}
                      onChange={(e) => setCode(e.target.value)}
                      inputMode="numeric"
                      autoFocus
                    />
                  </div>
                  <div className="flex gap-2">
                    <Button type="submit" disabled={code === ""}>
                      Verify
                    </Button>
                    <Button type="button" variant="ghost" onClick={() => setAsking(false)}>
                      Cancel
                    </Button>
                  </div>
                </form>
              ) : (
                <form className="flex flex-col gap-3" onSubmit={(e) => void signIn(e)}>
                  <div className="flex flex-col gap-1.5">
                    <Label htmlFor="username">Email</Label>
                    <Input
                      id="username"
                      type="email"
                      autoComplete="username"
                      value={username}
                      onChange={(e) => setUsername(e.target.value)}
                      autoFocus
                    />
                  </div>
                  <div className="flex flex-col gap-1.5">
                    <Label htmlFor="password">Password</Label>
                    <Input
                      id="password"
                      type="password"
                      autoComplete="current-password"
                      value={password}
                      onChange={(e) => setPassword(e.target.value)}
                    />
                  </div>
                  <div className="flex gap-2">
                    <Button type="submit" disabled={username === "" || password === ""}>
                      Sign in
                    </Button>
                    <Button type="button" variant="ghost" onClick={() => setAsking(false)}>
                      Cancel
                    </Button>
                  </div>
                </form>
              )}
            </CardContent>
          </Card>
        </div>
      ) : null}
    </main>
  );
}

export default App;
