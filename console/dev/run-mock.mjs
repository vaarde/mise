// Starts the local mock API and the Vite dev server pointed at it.
// Usage: npm run dev:mock
//
// Picks a free port for the mock so it never collides with another run, and
// stops both processes together on Ctrl+C.

import { spawn } from "node:child_process";
import net from "node:net";
import { fileURLToPath } from "node:url";

const here = fileURLToPath(new URL(".", import.meta.url));
const viteBin = fileURLToPath(new URL("../node_modules/vite/bin/vite.js", import.meta.url));

function freePort(preferred) {
  return new Promise((resolve) => {
    const server = net.createServer();
    server.once("error", () => resolve(freePort(0)));
    server.listen(preferred, () => {
      const { port } = server.address();
      server.close(() => resolve(port));
    });
  });
}

const children = [];
let stopping = false;

function stop(code = 0) {
  if (stopping) return;
  stopping = true;
  for (const child of children) if (!child.killed) child.kill();
  process.exit(code);
}

function run(args, env) {
  // Run node directly, never through a shell, so killing the child kills the server.
  const child = spawn(process.execPath, args, { stdio: ["ignore", "inherit", "inherit"], env: { ...process.env, ...env } });
  children.push(child);
  child.on("exit", (code) => stop(code ?? 0));
  return child;
}

process.on("SIGINT", () => stop());
process.on("SIGTERM", () => stop());

const port = await freePort(Number(process.env.MOCK_PORT || 8787));
run([`${here}mock-api.mjs`], { MOCK_PORT: String(port) });
run([viteBin], { VITE_API_BASE_URL: `http://localhost:${port}`, VITE_MOCK_API: "1" });
