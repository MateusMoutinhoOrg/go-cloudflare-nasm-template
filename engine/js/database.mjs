// Durable Object behind deps.DatabaseHub (see engine/worker/database.go).
//
// Each database id is one instance of this class, and each instance owns a
// private SQLite database. Cloudflare creates the instance the first time its
// id is used, so there is nothing to provision or migrate.

import { DurableObject } from "cloudflare:workers";

// A database that has this table was already created (see create).
const CREATED_TABLE = "_engine_created";

// How long callDatabase keeps trying while the database is locked.
const LOCK_TIMEOUT_MS = 5000;

// Calls a method of the database with this id. This is how the Worker reaches
// a database: `databases` is its DATABASES binding (see wrangler.jsonc).
//
// If the database is locked, waits and tries again. On Cloudflare that never
// happens, because one object is the only user of its database. It happens on
// your own machine when several instances of the server share the database
// files (scripts/system-ctl-install.py --port 3000:3005): SQLite lets one of
// them write at a time and refuses the others right away ("SQLITE_BUSY").
// Trying again is safe: every method of the class below is one transaction,
// so a call that was refused changed nothing.
export async function callDatabase(databases, id, method, args) {
  const deadline = Date.now() + LOCK_TIMEOUT_MS;
  for (let wait = 2; ; wait = Math.min(wait * 2, 100)) {
    try {
      return await databases.get(databases.idFromName(id))[method](...args);
    } catch (error) {
      const locked = String(error?.message).includes("SQLITE_BUSY");
      if (!locked || Date.now() >= deadline) throw error;
      await scheduler.wait(wait * (0.5 + Math.random())); // not everyone at the same moment
    }
  }
}

export class SandboxDatabase extends DurableObject {
  #created = false; // true once this object has seen its database created

  // Runs creationQuery only the first time this database is used.
  // (Not called "connect": every Durable Object already has a connect(), for sockets.)
  create(creationQuery) {
    if (this.#created) return;
    const { sql, kv } = this.ctx.storage;

    // All or nothing: a creation query that fails halfway leaves no trace,
    // so it runs again (from the start) next time.
    this.ctx.storage.transactionSync(() => {
      // The mark is a table, read with SQL, so that every local instance of
      // the server sees it. A value in storage.kv would not do: an instance
      // that opened the database before another one created it never sees
      // the value, and runs the creation query a second time.
      const mark = sql.exec("SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = ?", CREATED_TABLE);
      if (mark.toArray().length > 0) return;

      // Databases created before CREATED_TABLE existed were marked in
      // storage.kv: those only get the table.
      if (!kv.get("created") && creationQuery.trim()) sql.exec(creationQuery);
      sql.exec(`CREATE TABLE ${CREATED_TABLE} (at TEXT)`);
      sql.exec(`INSERT INTO ${CREATED_TABLE} VALUES (?)`, new Date().toISOString());
    });
    this.#created = true;
  }

  // Runs the query and returns every row: { columns: [name], rows: [[value]] }.
  all(query, args) {
    const { sql } = this.ctx.storage;
    return this.ctx.storage.transactionSync(() => {
      const cursor = sql.exec(query, ...args);
      return { columns: cursor.columnNames, rows: Array.from(cursor.raw()) };
    });
  }

  // Runs the query and returns how many rows it changed.
  run(query, args) {
    const { sql } = this.ctx.storage;
    const changes = () => sql.exec("SELECT total_changes() AS total").one().total;

    return this.ctx.storage.transactionSync(() => {
      const before = changes();
      sql.exec(query, ...args).toArray(); // toArray: runs the query to the end
      return changes() - before;
    });
  }
}
