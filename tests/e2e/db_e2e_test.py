#!/usr/bin/env python3
"""End-to-end database backup & restore test suite for DockVault.

For each enabled database engine (postgres, mariadb, redis), validates:
  1. Engine auto-detection (detectedDb) via Docker inspection.
  2. Seeding realistic multi-type schemas/tables/keys + baseline queries.
  3. Zero-downtime hot dump backup (dbHotDump=true, pausedContainers=[]).
  4. Inspection & extraction of db_dumps/<container>-<engine>.<ext> inside archive.
  5. Path A (Volume Restore): corrupt DB state, stop container, restore volume via DockVault,
     restart container, verify corrupted state is gone, all seeded data matches, AND new
     read/write transactions succeed.
  6. Path B (Hot-Dump Disaster Recovery): boot a brand-new clean database container, restore
     directly from the extracted db_dumps/ SQL/RDB artifact, verify all seeded data matches,
     AND verify new read/write transactions work.
"""

import json
import os
import subprocess
import time
import urllib.error
import urllib.parse
import urllib.request

from e2e_ui import Suite

BASE = os.environ.get("DOCKVAULT_DB_E2E_URL", "http://127.0.0.1:18089/api").rstrip("/")
PREFIX = os.environ.get("DOCKVAULT_DB_E2E_PREFIX", "dv-db-e2e")
WORKDIR = os.environ.get("DOCKVAULT_DB_E2E_WORKDIR", "/tmp")
ENGINES = [
    e.strip().lower()
    for e in os.environ.get("DOCKVAULT_DB_ENGINES", "postgres,mariadb,redis").split(",")
    if e.strip()
]


def req(method, path, body=None, timeout=60):
    url = BASE + path
    data = None
    headers = {}
    if body is not None:
        data = json.dumps(body).encode("utf-8")
        headers["Content-Type"] = "application/json"
    r = urllib.request.Request(url, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(r, timeout=timeout) as resp:
            raw = resp.read()
            ctype = resp.headers.get("Content-Type", "")
            return resp.status, (json.loads(raw.decode("utf-8")) if "application/json" in ctype else raw)
    except urllib.error.HTTPError as e:
        raw = e.read().decode("utf-8", errors="replace")
        raise RuntimeError(f"{method} {path} -> HTTP {e.code}: {raw}")


def run_cmd(args, input_bytes=None):
    try:
        return subprocess.check_output(args, input=input_bytes, stderr=subprocess.STDOUT)
    except subprocess.CalledProcessError as exc:
        out = exc.output.decode("utf-8", errors="replace") if exc.output else ""
        raise RuntimeError(f"Command {args} failed ({exc.returncode}): {out}") from exc


def wait_until(desc, fn, timeout_sec=90, interval=0.8):
    deadline = time.time() + timeout_sec
    last_err = None
    while time.time() < deadline:
        try:
            if fn():
                return
        except Exception as exc:
            last_err = exc
        time.sleep(interval)
    raise TimeoutError(f"Timed out waiting for {desc}: {last_err}")


def wait_backup(backup_id, timeout_sec=90):
    deadline = time.time() + timeout_sec
    last_job = None
    while time.time() < deadline:
        _, prog = req("GET", f"/backups/{backup_id}/progress")
        job = prog["job"]
        last_job = job
        if job["status"] in ("success", "failed"):
            if job["status"] != "success":
                raise AssertionError(f"Backup {backup_id} failed: {job}")
            _, detail = req("GET", "/backups?limit=100")
            for b in detail["backups"]:
                if b["id"] == backup_id:
                    return b, job
            return {"id": backup_id}, job
        time.sleep(0.4)
    raise TimeoutError(f"Backup {backup_id} timed out; last={last_job}")


def find_container(name):
    _, data = req("GET", "/containers?all=true")
    for c in data["containers"]:
        if c["name"] == name:
            return c
    raise AssertionError(f"Container {name} not discovered in {[c['name'] for c in data['containers']]}")


def get_default_storage_id():
    _, data = req("GET", "/storage")
    return data["providers"][0]["id"]


# ---------------------------------------------------------------------------
# 1. PostgreSQL E2E (postgres:16-alpine)
# ---------------------------------------------------------------------------
def test_postgres(local_id):
    pg_c = f"{PREFIX}-pg"
    pg_rec_c = f"{PREFIX}-pg-rec"
    pg_v = f"{PREFIX}-pg-vol"
    pg_rec_v = f"{PREFIX}-pg-rec-vol"

    run_cmd(["docker", "volume", "create", pg_v])
    run_cmd(["docker", "volume", "create", pg_rec_v])
    run_cmd([
        "docker", "run", "-d", "--name", pg_c,
        "-v", f"{pg_v}:/var/lib/postgresql/data",
        "-e", "POSTGRES_USER=appuser",
        "-e", "POSTGRES_PASSWORD=pgsecret",
        "-e", "POSTGRES_DB=ordersdb",
        "postgres:16-alpine",
    ])

    def pg_ready(cname):
        return subprocess.call(
            ["docker", "exec", cname, "pg_isready", "-U", "appuser", "-d", "ordersdb"],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        ) == 0

    def psql(cname, db, sql):
        return run_cmd(
            ["docker", "exec", "-e", "PGPASSWORD=pgsecret", cname, "psql", "-U", "appuser", "-d", db, "-At", "-v", "ON_ERROR_STOP=1", "-c", sql]
        ).decode("utf-8").strip()

    wait_until("postgres ready", lambda: pg_ready(pg_c))

    # Seed schema + relational data + sequence + index
    psql(pg_c, "ordersdb", """
        CREATE TABLE customers (
            id SERIAL PRIMARY KEY,
            name TEXT NOT NULL UNIQUE
        );
        CREATE TABLE orders (
            id SERIAL PRIMARY KEY,
            customer_id INT NOT NULL REFERENCES customers(id),
            sku TEXT NOT NULL,
            qty INT NOT NULL
        );
        INSERT INTO customers (name) VALUES ('Alice'), ('Bob');
        INSERT INTO orders (customer_id, sku, qty) VALUES (1, 'vault-widget', 42), (2, 'backup-gear', 99);
        CHECKPOINT;
    """)

    c_info = find_container(pg_c)
    assert c_info.get("detectedDb") == "postgres", f"expected detectedDb=postgres, got {c_info}"

    # Run zero-downtime hot dump backup
    _, start_res = req("POST", "/backups/run", {
        "targetType": "container",
        "targetId": c_info["id"],
        "storageProviderId": local_id,
        "compression": "zstd",
        "dbHotDump": True,
        "pauseContainer": True,
    })
    b_pg, _ = wait_backup(start_res["backup"]["id"])
    paused = json.loads(b_pg.get("pausedContainersJson") or "[]") or []
    assert paused == [], f"expected zero-downtime (no pause) for postgres hot dump, got paused={paused}"

    # Verify db_dumps/*.sql inside archive and extract it
    _, insp = req("GET", f"/backups/{b_pg['id']}/inspect")
    sql_entries = [e for e in insp["entries"] if "db_dumps/" in e["name"] and e["name"].endswith("-postgres.sql")]
    assert len(sql_entries) == 1 and sql_entries[0]["size"] > 0, f"missing postgres dump: {insp['entries']}"
    sql_path = sql_entries[0]["name"]
    _, sql_bytes = req("GET", f"/backups/{b_pg['id']}/file?path={urllib.parse.quote(sql_path)}")
    sql_text = sql_bytes.decode("utf-8")
    assert "vault-widget" in sql_text and "CREATE TABLE" in sql_text, "extracted SQL dump missing expected schema/data"

    # Path A: Corrupt live DB, stop container cleanly, restore volume, restart, verify read/write health
    psql(pg_c, "ordersdb", """
        DROP TABLE orders, customers CASCADE;
        CREATE TABLE corrupted_marker (note TEXT);
        INSERT INTO corrupted_marker VALUES ('corrupted');
        CHECKPOINT;
    """)
    run_cmd(["docker", "stop", pg_c])
    req("POST", f"/backups/{b_pg['id']}/restore", {"pauseContainer": False})
    run_cmd(["docker", "start", pg_c])
    wait_until("postgres ready after volume restore", lambda: pg_ready(pg_c))

    tbl_check = psql(pg_c, "ordersdb", "SELECT to_regclass('public.corrupted_marker')::text;")
    assert tbl_check == "", f"corrupted_marker still present after restore: {tbl_check!r}"
    rows = psql(pg_c, "ordersdb", "SELECT c.name || ':' || o.sku || ':' || o.qty FROM orders o JOIN customers c ON c.id=o.customer_id ORDER BY o.id;")
    assert rows == "Alice:vault-widget:42\nBob:backup-gear:99", f"unexpected postgres restored rows: {rows!r}"

    # Post-restore write check (sequences + FK + INSERT/UPDATE)
    psql(pg_c, "ordersdb", """
        INSERT INTO customers (name) VALUES ('Charlie');
        INSERT INTO orders (customer_id, sku, qty) VALUES ((SELECT id FROM customers WHERE name='Charlie'), 'post-restore-ok', 7);
        UPDATE orders SET qty = 43 WHERE sku = 'vault-widget';
    """)
    after_write = psql(pg_c, "ordersdb", "SELECT count(*), sum(qty) FROM orders;")
    assert after_write == "3|149", f"post-restore write check failed: {after_write!r}"

    # Path B: Disaster recovery from extracted pg_dumpall SQL into brand-new clean Postgres container
    run_cmd([
        "docker", "run", "-d", "--name", pg_rec_c,
        "-v", f"{pg_rec_v}:/var/lib/postgresql/data",
        "-e", "POSTGRES_USER=appuser",
        "-e", "POSTGRES_PASSWORD=pgsecret",
        "-e", "POSTGRES_DB=postgres",
        "postgres:16-alpine",
    ])
    wait_until("recovery postgres ready", lambda: pg_ready(pg_rec_c))
    # Standard pg_dumpall restore emits CREATE ROLE for the cluster bootstrap user, so run without ON_ERROR_STOP=1.
    subprocess.run(
        ["docker", "exec", "-i", "-e", "PGPASSWORD=pgsecret", pg_rec_c, "psql", "-U", "appuser", "-d", "postgres"],
        input=sql_bytes,
        check=True,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )
    rec_rows = psql(pg_rec_c, "ordersdb", "SELECT c.name || ':' || o.sku || ':' || o.qty FROM orders o JOIN customers c ON c.id=o.customer_id ORDER BY o.id;")
    assert rec_rows == "Alice:vault-widget:42\nBob:backup-gear:99", f"SQL recovery mismatch: {rec_rows!r}"
    psql(pg_rec_c, "ordersdb", "INSERT INTO customers (name) VALUES ('RecoveredUser');")
    assert psql(pg_rec_c, "ordersdb", "SELECT count(*) FROM customers;") == "3"
    return f"dump={len(sql_bytes)} B, volume restore + R/W + fresh SQL DR ok"


# ---------------------------------------------------------------------------
# 2. MariaDB E2E (mariadb:11)
# ---------------------------------------------------------------------------
def test_mariadb(local_id):
    mdb_c = f"{PREFIX}-mariadb"
    mdb_rec_c = f"{PREFIX}-mariadb-rec"
    mdb_v = f"{PREFIX}-mariadb-vol"
    mdb_rec_v = f"{PREFIX}-mariadb-rec-vol"

    run_cmd(["docker", "volume", "create", mdb_v])
    run_cmd(["docker", "volume", "create", mdb_rec_v])
    run_cmd([
        "docker", "run", "-d", "--name", mdb_c,
        "-v", f"{mdb_v}:/var/lib/mysql",
        "-e", "MARIADB_ROOT_PASSWORD=rootsecret",
        "-e", "MARIADB_DATABASE=shopdb",
        "mariadb:11",
    ])

    def mdb_ready(cname, db=None):
        cmd = ["docker", "exec", cname, "mariadb", "-uroot", "-prootsecret"]
        if db:
            cmd.append(db)
        cmd.extend(["-e", "SELECT 1"])
        return subprocess.call(
            cmd,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        ) == 0

    def msql(cname, db, sql):
        args = ["docker", "exec", cname, "mariadb", "-uroot", "-prootsecret", "-N", "-B"]
        if db:
            args.append(db)
        args.extend(["-e", sql])
        return run_cmd(args).decode("utf-8").strip()

    wait_until("mariadb ready", lambda: mdb_ready(mdb_c, "shopdb"), timeout_sec=120)

    msql(mdb_c, "shopdb", """
        CREATE TABLE products (
            id INT AUTO_INCREMENT PRIMARY KEY,
            code VARCHAR(64) NOT NULL UNIQUE,
            stock INT NOT NULL
        ) ENGINE=InnoDB;
        INSERT INTO products (code, stock) VALUES ('vault-disk', 15), ('crypto-key', 8);
        FLUSH TABLES;
    """)

    c_info = find_container(mdb_c)
    assert c_info.get("detectedDb") == "mariadb", f"expected detectedDb=mariadb, got {c_info}"

    _, start_res = req("POST", "/backups/run", {
        "targetType": "container",
        "targetId": c_info["id"],
        "storageProviderId": local_id,
        "compression": "zstd",
        "dbHotDump": True,
        "pauseContainer": True,
    })
    b_mdb, _ = wait_backup(start_res["backup"]["id"])
    paused = json.loads(b_mdb.get("pausedContainersJson") or "[]") or []
    assert paused == [], f"expected zero-downtime for mariadb hot dump, got paused={paused}"

    _, insp = req("GET", f"/backups/{b_mdb['id']}/inspect")
    sql_entries = [e for e in insp["entries"] if "db_dumps/" in e["name"] and e["name"].endswith("-mariadb.sql")]
    assert len(sql_entries) == 1 and sql_entries[0]["size"] > 0, f"missing mariadb dump: {insp['entries']}"
    _, sql_bytes = req("GET", f"/backups/{b_mdb['id']}/file?path={urllib.parse.quote(sql_entries[0]['name'])}")
    assert "vault-disk" in sql_bytes.decode("utf-8")

    # Path A: Corrupt DB, stop container cleanly, restore volume, start container, verify read/write health
    msql(mdb_c, "shopdb", "DROP TABLE products; CREATE TABLE corrupted_tbl (x INT); INSERT INTO corrupted_tbl VALUES (1);")
    run_cmd(["docker", "stop", mdb_c])
    req("POST", f"/backups/{b_mdb['id']}/restore", {"pauseContainer": False})
    run_cmd(["docker", "start", mdb_c])
    wait_until("mariadb ready after volume restore", lambda: mdb_ready(mdb_c, "shopdb"), timeout_sec=120)

    bad_check = msql(mdb_c, "shopdb", "SHOW TABLES LIKE 'corrupted_tbl';")
    assert bad_check == "", f"corrupted_tbl survived volume restore: {bad_check!r}"
    rows = msql(mdb_c, "shopdb", "SELECT CONCAT(id, ':', code, ':', stock) FROM products ORDER BY id;")
    assert rows == "1:vault-disk:15\n2:crypto-key:8", f"mariadb restored rows mismatch: {rows!r}"

    msql(mdb_c, "shopdb", "INSERT INTO products (code, stock) VALUES ('restored-item', 3); UPDATE products SET stock = 16 WHERE code = 'vault-disk';")
    summary = msql(mdb_c, "shopdb", "SELECT COUNT(*), SUM(stock) FROM products;")
    assert summary == "3\t27", f"mariadb post-restore write check failed: {summary!r}"

    # Path B: Logical SQL hot-dump recovery into brand-new MariaDB instance
    run_cmd([
        "docker", "run", "-d", "--name", mdb_rec_c,
        "-v", f"{mdb_rec_v}:/var/lib/mysql",
        "-e", "MARIADB_ROOT_PASSWORD=rootsecret",
        "mariadb:11",
    ])
    wait_until("recovery mariadb ready", lambda: mdb_ready(mdb_rec_c), timeout_sec=120)
    subprocess.run(
        ["docker", "exec", "-i", mdb_rec_c, "mariadb", "-uroot", "-prootsecret"],
        input=sql_bytes,
        check=True,
        stdout=subprocess.DEVNULL,
    )
    rec_rows = msql(mdb_rec_c, "shopdb", "SELECT CONCAT(id, ':', code, ':', stock) FROM products ORDER BY id;")
    assert rec_rows == "1:vault-disk:15\n2:crypto-key:8", f"mariadb SQL recovery mismatch: {rec_rows!r}"
    msql(mdb_rec_c, "shopdb", "INSERT INTO products (code, stock) VALUES ('dr-ok', 5);")
    assert msql(mdb_rec_c, "shopdb", "SELECT COUNT(*) FROM products;") == "3"
    return f"dump={len(sql_bytes)} B, volume restore + R/W + fresh SQL DR ok"


# ---------------------------------------------------------------------------
# 3. Redis E2E (redis:7-alpine)
# ---------------------------------------------------------------------------
def test_redis(local_id):
    rd_c = f"{PREFIX}-redis"
    rd_rec_c = f"{PREFIX}-redis-rec"
    rd_v = f"{PREFIX}-redis-vol"
    rd_rec_v = f"{PREFIX}-redis-rec-vol"

    run_cmd(["docker", "volume", "create", rd_v])
    run_cmd(["docker", "volume", "create", rd_rec_v])
    run_cmd([
        "docker", "run", "-d", "--name", rd_c,
        "-v", f"{rd_v}:/data",
        "redis:7-alpine",
        "redis-server", "--save", "1", "1", "--appendonly", "no",
    ])

    def rd_ready(cname):
        return subprocess.call(
            ["docker", "exec", cname, "redis-cli", "PING"],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        ) == 0

    def rcli(cname, *args):
        return run_cmd(["docker", "exec", cname, "redis-cli", *args]).decode("utf-8").strip()

    wait_until("redis ready", lambda: rd_ready(rd_c))

    rcli(rd_c, "SET", "app:version", "v0.3.0")
    rcli(rd_c, "HSET", "user:1", "role", "admin", "status", "active")
    rcli(rd_c, "RPUSH", "queue:jobs", "job-a", "job-b")
    rcli(rd_c, "ZADD", "scores", "10", "alice", "25", "bob")
    rcli(rd_c, "SAVE")

    c_info = find_container(rd_c)
    assert c_info.get("detectedDb") == "redis", f"expected detectedDb=redis, got {c_info}"

    _, start_res = req("POST", "/backups/run", {
        "targetType": "container",
        "targetId": c_info["id"],
        "storageProviderId": local_id,
        "compression": "gzip",
        "dbHotDump": True,
        "pauseContainer": True,
    })
    b_rd, _ = wait_backup(start_res["backup"]["id"])
    paused = json.loads(b_rd.get("pausedContainersJson") or "[]") or []
    assert paused == [], f"expected zero-downtime for redis hot dump, got paused={paused}"

    _, insp = req("GET", f"/backups/{b_rd['id']}/inspect")
    rdb_entries = [e for e in insp["entries"] if "db_dumps/" in e["name"] and e["name"].endswith("-redis.rdb")]
    assert len(rdb_entries) == 1 and rdb_entries[0]["size"] > 0
    _, rdb_bytes = req("GET", f"/backups/{b_rd['id']}/file?path={urllib.parse.quote(rdb_entries[0]['name'])}")
    assert rdb_bytes[:5] == b"REDIS", f"invalid RDB header: {rdb_bytes[:10]!r}"

    # Path A: Flush/corrupt Redis, stop container, restore volume, restart container, verify keys & writes
    rcli(rd_c, "FLUSHALL")
    rcli(rd_c, "SET", "evil:key", "corrupted")
    rcli(rd_c, "SAVE")
    run_cmd(["docker", "stop", rd_c])
    req("POST", f"/backups/{b_rd['id']}/restore", {"pauseContainer": False})
    run_cmd(["docker", "start", rd_c])
    wait_until("redis ready after volume restore", lambda: rd_ready(rd_c))

    assert rcli(rd_c, "GET", "evil:key") == "", "corrupted key survived redis restore"
    assert rcli(rd_c, "GET", "app:version") == "v0.3.0"
    assert rcli(rd_c, "HGET", "user:1", "role") == "admin"
    assert rcli(rd_c, "LRANGE", "queue:jobs", "0", "-1") == "job-a\njob-b"
    assert rcli(rd_c, "ZSCORE", "scores", "bob") == "25"

    # Post-restore writes work
    rcli(rd_c, "SET", "post:restore", "ok")
    assert rcli(rd_c, "GET", "post:restore") == "ok"

    # Path B: Restore extracted hot dump .rdb into brand-new clean Redis container
    rdb_host_file = os.path.join(WORKDIR, "dumps", "dump.rdb")
    with open(rdb_host_file, "wb") as f:
        f.write(rdb_bytes)
    run_cmd([
        "docker", "run", "--rm",
        "-v", f"{rd_rec_v}:/data",
        "-v", f"{rdb_host_file}:/tmp/dump.rdb:ro",
        "alpine:3.21", "cp", "/tmp/dump.rdb", "/data/dump.rdb",
    ])
    run_cmd([
        "docker", "run", "-d", "--name", rd_rec_c,
        "-v", f"{rd_rec_v}:/data",
        "redis:7-alpine",
    ])
    wait_until("recovery redis ready", lambda: rd_ready(rd_rec_c))
    assert rcli(rd_rec_c, "GET", "app:version") == "v0.3.0"
    assert rcli(rd_rec_c, "HGET", "user:1", "status") == "active"
    rcli(rd_rec_c, "SET", "dr:write", "healthy")
    assert rcli(rd_rec_c, "GET", "dr:write") == "healthy"
    return f"rdb={len(rdb_bytes)} B, volume restore + R/W + fresh RDB DR ok"


def main():
    local_id = get_default_storage_id()
    runners = {
        "postgres": ("PostgreSQL 16 (hot dump, volume & SQL DR)", test_postgres),
        "mariadb": ("MariaDB 11 (hot dump, volume & SQL DR)", test_mariadb),
        "redis": ("Redis 7 (hot dump, volume & RDB DR)", test_redis),
    }
    unknown = [e for e in ENGINES if e not in runners]
    if unknown:
        raise SystemExit(f"Unsupported engine(s): {unknown} (supported: {list(runners)})")

    suite = Suite("Database E2E Suite", len(ENGINES))
    for idx, eng in enumerate(ENGINES, start=1):
        label, fn = runners[eng]
        with suite.step(idx, label) as note:
            note(fn(local_id))
    suite.finish()


if __name__ == "__main__":
    main()
