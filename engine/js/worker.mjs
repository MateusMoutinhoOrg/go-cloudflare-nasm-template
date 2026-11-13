// Cloudflare Worker entry point.
//
// For each request: creates a fresh instance of the Go program (engine/worker
// compiled to WebAssembly) and hands it a "bridge": the request, plus the
// functions Go calls to send the response and to reach the databases.

import "../build/wasm_exec.js"; // Go's JS runtime (copied from GOROOT by compile.sh)
import goProgram from "../build/server.wasm"; // compiled Go program (WebAssembly.Module)
import { callDatabase } from "./database.mjs";

export { SandboxDatabase } from "./database.mjs"; // Durable Object: one SQLite database per id

// Bridge of each in-flight request, read by Go in engine/worker/main.go.
const bridges = new Map();
globalThis.__goRequests = bridges;

function plainText(status, body) {
  return new Response(body, {
    status,
    headers: { "content-type": "text/plain; charset=utf-8" },
  });
}

export default {
  async fetch(request, env, ctx) {
    const id = crypto.randomUUID();
    let body = null; // writer of the response body, while the body is open

    const response = new Promise((resolve) => {
      bridges.set(id, {
        request,

        // Sends the status and the headers ([[name, value], ...]) to the client.
        // Throws if they are not valid (e.g. status 999).
        start(status, headers, hasBody) {
          if (!hasBody) {
            resolve(new Response(null, { status, headers }));
            return;
          }
          const { readable, writable } = new TransformStream();
          resolve(new Response(readable, { status, headers }));
          body = writable.getWriter();
        },

        // Sends a piece of the body (Uint8Array). Resolves once it was taken.
        write: (chunk) => body.write(chunk),

        // Finishes the body.
        end() {
          body.close().catch(() => {}); // the client may be gone already
          body = null;
        },

        // Calls a method of the database with this id (see database.mjs).
        database: (databaseId, method, ...args) =>
          callDatabase(env.DATABASES, databaseId, method, args),
      });
    });

    try {
      const go = new Go();
      go.env = { REQUEST_ID: id };
      const instance = await WebAssembly.instantiate(goProgram, go.importObject);

      const goExit = go.run(instance).then(
        () => "The Go program exited",
        (error) => `The Go program failed (${error})`,
      ).then((problem) => {
        // Go is gone: a body it left open is cut short, so the client can
        // tell that the response is incomplete.
        body?.abort(new Error(`${problem} before finishing the response body.`)).catch(() => {});
        return plainText(500, `${problem} without responding.`);
      });

      // The Go program keeps writing the body after the response is returned.
      ctx.waitUntil(goExit);

      // Go normally responds first; goExit only wins when it never does.
      return await Promise.race([response, goExit]);
    } finally {
      bridges.delete(id);
    }
  },
};
