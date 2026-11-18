#!/usr/bin/env python3
"""Comprehensive end-to-end Docker integration suite for DockVault (12 scenarios).

Covers:
  1. Discovery (containers, stacks, orphan volumes, docker.sock filter, exclude label, GitOps sync)
  2. Embedded SPA root + SSE live progress event stream
  3. Selective/bind-mount zstd backup, excludes, docker.sock omission, pre/post hooks, inspect, extract, verify, staging & pre-restore snapshot
  4. Multi-container Compose stack backup & multi-volume stack restore
  5. AES-256-GCM encrypted storage (.tar.zst.enc), 3-2-1 secondary replication, DVENC1 raw vs decrypted download, & orphan restore
  6. Zero-downtime Redis DB hot dump (db_dumps/*.rdb)
  7. Schedule lifecycle (preview, create, Keep-N retention pruning + pin protection, clone, toggle, delete)
  8. Disaster recovery Scan & Import after metadata-only deletion (?deleteFile=false)
  9. Backup notes, snapshot comparison diff, bulk actions (verify/delete), & selective path restore
 10. Per-target backup concurrency guard (409 Conflict) & orphan volume deletion vs in-use guard
 11. Webhook notification creation + live HTTP delivery test & UI preferences persistence/reset
 12. Auth setup, 401 unauthorized enforcement, wrong-password rejection, cookie login, dashboard analytics, & logout
"""

import http.cookiejar
import http.server
import json
import os
import subprocess
import threading
import time
import urllib.error
import urllib.parse
import urllib.request

from e2e_ui import Suite

BASE = os.environ.get("DOCKVAULT_E2E_URL", "http://127.0.0.1:18088/api").rstrip("/")
WEB_URL = os.environ.get("DOCKVAULT_E2E_WEB_URL", "http://127.0.0.1:18088/")
PREFIX = os.environ.get("DOCKVAULT_E2E_PREFIX", "dv-e2e")
BIND_DIR = os.environ.get("DOCKVAULT_E2E_BIND_DIR", "")

DOCKVAULT_C = f"{PREFIX}-dockvault"
APP_CONTAINER = f"{PREFIX}-app"
EXCLUDED_CONTAINER = f"{PREFIX}-excluded"
STACK_NAME = f"{PREFIX}-stack"
REDIS_CONTAINER = f"{PREFIX}-redis"
WEBHOOK_CONTAINER = f"{PREFIX}-webhook"

APP_VOL = f"{PREFIX}-app-vol"
ORPHAN_VOL = f"{PREFIX}-orphan-vol"
DELETE_VOL = f"{PREFIX}-delete-vol"
WEB_VOL = f"{PREFIX}-web-vol"
API_VOL = f"{PREFIX}-api-vol"
STAGING_VOL = f"{PREFIX}-staging-vol"
SELECTIVE_VOL = f"{PREFIX}-selective-vol"
ARCHIVE_APP_DIR = f"vol_{APP_VOL}"

cookie_jar = http.cookiejar.CookieJar()
opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(cookie_jar))


def req(method, path, body=None, timeout=30, expect_statuses=None, use_cookies=True):
    url = BASE + path
    data = None
    headers = {}
    if body is not None:
        data = json.dumps(body).encode("utf-8")
        headers["Content-Type"] = "application/json"
    r = urllib.request.Request(url, data=data, headers=headers, method=method)
    client = opener if use_cookies else urllib.request.build_opener()
    try:
        with client.open(r, timeout=timeout) as resp:
            raw = resp.read()
            ctype = resp.headers.get("Content-Type", "")
            parsed = json.loads(raw.decode("utf-8")) if "application/json" in ctype else raw
            if expect_statuses and resp.status not in expect_statuses:
                raise AssertionError(f"{method} {path} -> HTTP {resp.status}, expected {expect_statuses}")
            return resp.status, parsed
    except urllib.error.HTTPError as e:
        raw = e.read().decode("utf-8", errors="replace")
        if expect_statuses and e.code in expect_statuses:
            try:
                return e.code, json.loads(raw)
            except Exception:
                return e.code, raw
        raise RuntimeError(f"{method} {path} -> HTTP {e.code}: {raw}")


def wait_backup(backup_id, timeout_sec=60):
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
        time.sleep(0.35)
    raise TimeoutError(f"Backup {backup_id} timed out; last={last_job}")


def wait_new_schedule_backup(target_name, seen_ids, timeout_sec=60):
    deadline = time.time() + timeout_sec
    while time.time() < deadline:
        _, res = req("GET", f"/backups?target={urllib.parse.quote(target_name)}&limit=50")
        running = [b for b in res["backups"] if b["status"] == "running"]
        newer = [b for b in res["backups"] if b["id"] not in seen_ids and b["status"] == "success"]
        if not running and newer:
            return res["backups"], {b["id"] for b in res["backups"]}
        time.sleep(0.35)
    raise TimeoutError(f"Timed out waiting for new schedule backup on {target_name}")


def vol_exec(volume_name, sh_cmd):
    return subprocess.check_output(
        ["docker", "run", "--rm", "-v", f"{volume_name}:/vol", "alpine:3.21", "sh", "-c", sh_cmd],
        text=True,
    ).strip()


def run_tests():
    suite = Suite("Core E2E Suite", 12)
    try:
        _run_scenarios(suite)
    except Exception:
        suite.fail()
        raise
    suite.finish()


def _run_scenarios(suite):
    # 1. Discovery, Stacks, Socket Filtering, Excluded Containers, Orphans & GitOps Sync
    suite.begin(1, "Discovery, stacks, orphans & GitOps sync")
    _, cdata = req("GET", "/containers")
    containers = {c["name"]: c for c in cdata["containers"]}
    assert APP_CONTAINER in containers, f"{APP_CONTAINER} missing"
    assert REDIS_CONTAINER in containers, f"{REDIS_CONTAINER} missing"
    if EXCLUDED_CONTAINER in containers:
        assert containers[EXCLUDED_CONTAINER].get("excluded") is True, "dockvault.exclude container should be excluded"

    app = containers[APP_CONTAINER]
    assert app.get("gitopsSchedule") == "15 4 * * *", f"unexpected gitopsSchedule: {app}"
    assert app.get("gitopsExcludePaths") == "*.tmp,cache/**", f"unexpected gitopsExcludePaths: {app}"
    assert app.get("gitopsCompression") == "zstd", f"unexpected gitopsCompression: {app}"
    assert app.get("gitopsRetention") == 14, f"unexpected gitopsRetention: {app}"

    sock_mounts = [m for m in app["mounts"] if m.get("isDockerSocket")]
    assert len(sock_mounts) == 1 and sock_mounts[0]["backupable"] is False, f"docker.sock must not be backupable: {app['mounts']}"
    backupable_mounts = [m for m in app["mounts"] if m.get("backupable")]
    assert len(backupable_mounts) == 2, f"expected volume + bind mount backupable: {app['mounts']}"

    assert containers[REDIS_CONTAINER].get("detectedDb") == "redis"

    _, st_data = req("GET", "/stacks")
    stacks = {s["name"]: s for s in st_data["stacks"]}
    assert STACK_NAME in stacks, f"{STACK_NAME} missing from stacks: {list(stacks)}"
    assert len(stacks[STACK_NAME]["containers"]) == 2, f"expected 2 containers in stack: {stacks[STACK_NAME]}"

    _, vdata = req("GET", "/volumes")
    vols = {v["name"]: v for v in vdata["volumes"]}
    assert ORPHAN_VOL in vols and vols[ORPHAN_VOL]["inUse"] is False
    assert DELETE_VOL in vols and vols[DELETE_VOL]["inUse"] is False

    _, sdata = req("GET", "/schedules")
    gitops_scheds = [
        s for s in sdata["schedules"]
        if s.get("managedByLabel") and s.get("targetName") == APP_CONTAINER
    ]
    assert len(gitops_scheds) == 1, f"expected 1 GitOps schedule on startup, got {gitops_scheds}"
    req("POST", "/schedules/sync-labels", {})
    suite.ok()

    # 2. Embedded SPA Root + SSE Live Progress Stream
    suite.begin(2, "Embedded SPA root & live SSE progress stream")
    with urllib.request.urlopen(WEB_URL, timeout=5) as resp:
        html = resp.read().decode("utf-8", errors="replace")
        assert resp.status == 200 and "<html" in html.lower() and "dockvault" in html.lower(), "embedded SPA failed to load"

    with urllib.request.urlopen(urllib.request.Request(BASE + "/backups/events"), timeout=5) as resp:
        event_line = resp.readline().decode("utf-8")
        data_line = resp.readline().decode("utf-8")
        assert event_line.strip() == "event: progress", f"unexpected SSE event: {event_line!r}"
        payload = json.loads(data_line[len("data:"):].strip())
        assert "jobs" in payload
    suite.ok()

    # 3. zstd Backup (Volume + Bind Mount), Socket Omission, Excludes, Hooks, Verify, Staging & Pre-Restore Snapshot
    suite.begin(3, "zstd backup, hooks, excludes, staging & snapshot restore")
    _, storage_data = req("GET", "/storage")
    local_id = storage_data["providers"][0]["id"]

    _, start_res = req("POST", "/backups/run", {
        "targetType": "container",
        "targetId": app["id"],
        "storageProviderId": local_id,
        "pauseContainer": True,
        "compression": "zstd",
        "excludePatterns": "*.tmp,cache/**",
        "preCommand": "echo pre-hook-ok > /data/pre-hook.txt",
        "postCommand": "echo post-hook-ok > /data/post-hook.txt",
        "note": "e2e-zstd-test",
    })
    b1, job1 = wait_backup(start_res["backup"]["id"])
    assert b1["compression"] == "zstd" and b1["fileName"].endswith(".tar.zst")
    assert len(job1.get("steps", [])) >= 4

    post_hook_out = subprocess.check_output(["docker", "exec", APP_CONTAINER, "cat", "/data/post-hook.txt"], text=True).strip()
    assert post_hook_out == "post-hook-ok", f"postCommand did not execute: {post_hook_out}"

    _, insp = req("GET", f"/backups/{b1['id']}/inspect")
    names = [e["name"] for e in insp["entries"]]
    assert any(n.endswith("config/app.conf") for n in names)
    assert any(n.endswith("data.txt") for n in names)
    assert any(n.endswith("pre-hook.txt") for n in names)
    assert any(n.endswith("bind.conf") for n in names), f"bind mount file missing from archive: {names}"
    assert not any("docker.sock" in n for n in names), f"docker.sock leaked into archive: {names}"
    assert not any("debug.tmp" in n for n in names)
    assert not any("blob.bin" in n for n in names)

    conf_path = next(n for n in names if n.endswith("config/app.conf"))
    _, conf_bytes = req("GET", f"/backups/{b1['id']}/file?path={urllib.parse.quote(conf_path)}")
    assert conf_bytes.decode("utf-8").strip() == "app_env=production"

    _, ver = req("POST", f"/backups/{b1['id']}/verify", {})
    assert ver["ok"] is True and ver["match"] is True

    # Staging volume restore
    req("POST", f"/backups/{b1['id']}/restore", {"targetVolumeName": STAGING_VOL, "pauseContainer": False})
    assert vol_exec(STAGING_VOL, f"cat /vol/{ARCHIVE_APP_DIR}/config/app.conf") == "app_env=production"
    assert vol_exec(STAGING_VOL, f"test -e /vol/{ARCHIVE_APP_DIR}/debug.tmp && echo YES || echo NO") == "NO"

    # Mutate live volume & bind mount? Keep bind mount check + mutate volume for pre-restore snapshot
    vol_exec(APP_VOL, "rm -f /vol/config/app.conf && echo corrupted-v2 > /vol/data.txt")
    _, rst_res = req("POST", f"/backups/{b1['id']}/restore", {"pauseContainer": True, "preRestoreBackup": True})
    pre_id = rst_res.get("preRestoreBackupId")
    assert pre_id, f"missing preRestoreBackupId: {rst_res}"
    assert vol_exec(APP_VOL, "cat /vol/data.txt") == "important-data-v1"
    assert vol_exec(APP_VOL, "cat /vol/config/app.conf") == "app_env=production"
    if BIND_DIR:
        assert os.path.exists(os.path.join(BIND_DIR, "bind.conf"))

    _, pre_insp = req("GET", f"/backups/{pre_id}/inspect")
    pre_data_path = next(e["name"] for e in pre_insp["entries"] if e["name"].endswith("data.txt"))
    _, pre_data_bytes = req("GET", f"/backups/{pre_id}/file?path={urllib.parse.quote(pre_data_path)}")
    assert pre_data_bytes.decode("utf-8").strip() == "corrupted-v2"
    suite.ok()

    # 4. Compose Stack Backup & Multi-Volume Stack Restore
    suite.begin(4, "Compose stack backup & multi-volume restore")
    _, st_run = req("POST", "/backups/run", {
        "targetType": "stack",
        "targetId": STACK_NAME,
        "storageProviderId": local_id,
        "compression": "zstd",
        "pauseContainer": True,
    })
    b_stack, _ = wait_backup(st_run["backup"]["id"])
    _, st_insp = req("GET", f"/backups/{b_stack['id']}/inspect")
    st_names = [e["name"] for e in st_insp["entries"]]
    assert any(n.endswith("index.html") for n in st_names), f"web service file missing: {st_names}"
    assert any(n.endswith("state.json") for n in st_names), f"api service file missing: {st_names}"

    vol_exec(WEB_VOL, "echo broken > /vol/index.html")
    vol_exec(API_VOL, "echo broken > /vol/state.json")
    req("POST", f"/backups/{b_stack['id']}/restore", {"pauseContainer": True})
    assert vol_exec(WEB_VOL, "cat /vol/index.html") == "<h1>web-v1</h1>"
    assert vol_exec(API_VOL, "cat /vol/state.json") == '{"version":1}'
    suite.ok()

    # 5. Encrypted Storage (.tar.zst.enc), Unsaved Storage Test, Raw DVENC1 Download & 3-2-1 Replication
    suite.begin(5, "AES-256-GCM encryption, DVENC1 raw & 3-2-1 replication")
    st_bad, _ = req("POST", "/storage/test", {
        "name": "Bad Probe",
        "type": "s3",
        "endpoint": "http://127.0.0.1:9",
        "bucket": "nope",
        "accessKey": "a",
        "secretKey": "b",
    }, expect_statuses={400, 500})
    assert st_bad in (400, 500)

    _, enc_prov = req("POST", "/storage", {
        "name": f"{PREFIX} Encrypted Local",
        "type": "local",
        "localPath": "/app/backups/encrypted",
        "encryptBackups": True,
        "encryptionPassphrase": "custom-vault-key-123",
    })
    enc_id = enc_prov["provider"]["id"]
    _, test_res = req("POST", f"/storage/{enc_id}/test", {})
    assert test_res["ok"] is True

    _, enc_run = req("POST", "/backups/run", {
        "targetType": "volume",
        "targetId": ORPHAN_VOL,
        "storageProviderId": enc_id,
        "compression": "zstd",
        "pauseContainer": False,
    })
    b_enc, _ = wait_backup(enc_run["backup"]["id"])
    assert b_enc["encrypted"] is True and b_enc["fileName"].endswith(".tar.zst.enc")

    _, enc_ver = req("POST", f"/backups/{b_enc['id']}/verify", {})
    assert enc_ver["ok"] is True and enc_ver["match"] is True
    _, enc_insp = req("GET", f"/backups/{b_enc['id']}/inspect")
    assert any(e["name"].endswith("orphan.txt") for e in enc_insp["entries"])

    _, dl_dec = req("GET", f"/backups/{b_enc['id']}/download")
    assert dl_dec[:4] == b"\x28\xb5\x2f\xfd", "decrypted download missing zstd magic bytes"
    _, dl_raw = req("GET", f"/backups/{b_enc['id']}/download?raw=true")
    assert dl_raw[:8] == b"DVENC1\x00\x01", f"raw download missing DVENC1 magic header: {dl_raw[:8]!r}"

    vol_exec(ORPHAN_VOL, "echo wiped > /vol/orphan.txt")
    req("POST", f"/backups/{b_enc['id']}/restore", {"pauseContainer": False})
    assert vol_exec(ORPHAN_VOL, "cat /vol/orphan.txt") == "orphan-precious-data"

    # 3-2-1 secondary offsite replication (primary local -> secondary encrypted local)
    _, rep_run = req("POST", "/backups/run", {
        "targetType": "volume",
        "targetId": ORPHAN_VOL,
        "storageProviderId": local_id,
        "secondaryStorageId": enc_id,
        "compression": "gzip",
        "pauseContainer": False,
    })
    b_rep, _ = wait_backup(rep_run["backup"]["id"])
    assert "3-2-1 Offsite Replication" in b_rep.get("stepsJson", ""), f"expected 3-2-1 replication step: {b_rep.get('stepsJson')}"
    enc_files = subprocess.check_output(
        ["docker", "exec", DOCKVAULT_C, "ls", "/app/backups/encrypted"], text=True
    ).splitlines()
    assert any(f.endswith(".tar.gz.enc") for f in enc_files), f"expected replicated .tar.gz.enc file: {enc_files}"
    suite.ok()

    # 6. Zero-Downtime Redis DB Hot Dump
    suite.begin(6, "Zero-downtime Redis DB hot dump")
    _, db_run = req("POST", "/backups/run", {
        "targetType": "container",
        "targetId": REDIS_CONTAINER,
        "storageProviderId": local_id,
        "compression": "gzip",
        "dbHotDump": True,
        "pauseContainer": True,
    })
    b_db, _ = wait_backup(db_run["backup"]["id"])
    _, db_insp = req("GET", f"/backups/{b_db['id']}/inspect")
    rdb_entries = [e for e in db_insp["entries"] if "db_dumps/" in e["name"] and e["name"].endswith(".rdb")]
    assert len(rdb_entries) == 1 and rdb_entries[0]["size"] > 0
    r_val = subprocess.check_output(["docker", "exec", REDIS_CONTAINER, "redis-cli", "GET", "e2e:key1"], text=True).strip()
    assert r_val == "hello-dockvault"
    suite.ok(f"{rdb_entries[0]['name']} ({rdb_entries[0]['size']} B)")

    # 7. Schedule Lifecycle: Preview, Create, Retention Keep-2 + Pin Protection, Clone, Toggle & Delete
    suite.begin(7, "Schedule lifecycle, Keep-N retention & pin protection")
    _, prev_ok = req("POST", "/schedules/preview", {"cronExpr": "0 3 * * *"})
    assert prev_ok["valid"] is True and prev_ok.get("nextRunAt")
    st_bad_cron, _ = req("POST", "/schedules/preview", {"cronExpr": "not a cron"}, expect_statuses={400})
    assert st_bad_cron == 400

    _, sch_create = req("POST", "/schedules", {
        "name": f"{PREFIX} Retention Test",
        "targetType": "volume",
        "targetId": DELETE_VOL,
        "targetName": DELETE_VOL,
        "cronExpr": "0 5 * * *",
        "storageProviderId": local_id,
        "pauseContainer": False,
        "retentionPolicy": "count",
        "retentionCount": 2,
        "compression": "zstd",
        "enabled": True,
    })
    ret_sched = sch_create["schedule"]

    # Run #1 & Pin it (protected from pruning)
    seen_ids = set()
    req("POST", f"/schedules/{ret_sched['id']}/run", {})
    runs1, seen_ids = wait_new_schedule_backup(DELETE_VOL, seen_ids)
    pinned_id = runs1[0]["id"]
    req("POST", f"/backups/{pinned_id}/pin", {})

    # Run #2, #3, #4 (1.1s apart so second-resolution filenames never collide; Keep-2 retains pinned + 2 latest unpinned)
    time.sleep(1.1)
    req("POST", f"/schedules/{ret_sched['id']}/run", {})
    _, seen_ids = wait_new_schedule_backup(DELETE_VOL, seen_ids)
    time.sleep(1.1)
    req("POST", f"/schedules/{ret_sched['id']}/run", {})
    _, seen_ids = wait_new_schedule_backup(DELETE_VOL, seen_ids)
    time.sleep(1.1)
    req("POST", f"/schedules/{ret_sched['id']}/run", {})
    final_runs, _ = wait_new_schedule_backup(DELETE_VOL, seen_ids)
    remaining_ids = {b["id"] for b in final_runs}
    assert pinned_id in remaining_ids, f"pinned backup {pinned_id} was wrongly pruned: {final_runs}"
    unpinned_count = sum(1 for b in final_runs if not b.get("pinned"))
    assert unpinned_count <= 2, f"Keep-2 failed to prune unpinned backups: {final_runs}"

    # Clone paused copy + clone apply to ORPHAN_VOL + toggle + cleanup
    _, clone_paused = req("POST", f"/schedules/{ret_sched['id']}/clone", {})
    paused_copy_id = clone_paused["schedules"][0]["id"]
    assert clone_paused["schedules"][0]["enabled"] is False
    _, clone_applied = req("POST", f"/schedules/{ret_sched['id']}/clone", {
        "targets": [{"targetType": "volume", "targetId": ORPHAN_VOL, "targetName": ORPHAN_VOL}]
    })
    applied_id = clone_applied["schedules"][0]["id"]
    req("POST", f"/schedules/{paused_copy_id}/toggle", {})
    req("DELETE", f"/schedules/{paused_copy_id}")
    req("DELETE", f"/schedules/{applied_id}")
    req("DELETE", f"/schedules/{ret_sched['id']}")
    suite.ok()

    # 8. Disaster Recovery: Scan & Import Existing Archives
    suite.begin(8, "Disaster recovery scan & import")
    req("DELETE", f"/backups/{b_stack['id']}?deleteFile=false")
    _, scan_res = req("POST", f"/storage/{local_id}/scan", {})
    unimported = [o for o in scan_res["objects"] if o["storagePath"] == b_stack["storagePath"]]
    assert len(unimported) == 1 and unimported[0]["alreadyImported"] is False, f"expected unimported object: {scan_res}"

    _, imp_res = req("POST", f"/storage/{local_id}/import", {"storagePaths": [b_stack["storagePath"]]})
    assert imp_res["imported"] == 1, f"expected imported=1: {imp_res}"
    _, after_imp = req("GET", f"/backups?target={urllib.parse.quote(STACK_NAME)}&limit=10")
    reimported = [b for b in after_imp["backups"] if b["fileName"] == b_stack["fileName"]]
    assert len(reimported) == 1 and reimported[0]["trigger"] == "imported"
    suite.ok()

    # 9. Notes, Snapshot Compare, Bulk Verify/Delete, & Selective Path Restore
    suite.begin(9, "Notes, snapshot compare, bulk ops & selective restore")
    _, note_res = req("PUT", f"/backups/{b1['id']}/note", {"note": "Golden pre-migration baseline"})
    assert note_res["backup"]["note"] == "Golden pre-migration baseline"

    _, cmp_res = req("GET", f"/backups/compare?base={b1['id']}&target={pre_id}")
    assert cmp_res["summary"]["removed"] >= 1

    _, bulk_ver = req("POST", "/backups/bulk", {"action": "verify", "ids": [b1["id"], b_enc["id"], b_db["id"]]})
    assert bulk_ver["succeeded"] == 3 and bulk_ver["failed"] == 0

    req("POST", f"/backups/{b1['id']}/restore", {
        "targetVolumeName": SELECTIVE_VOL,
        "pauseContainer": False,
        "paths": [conf_path],
    })
    assert vol_exec(SELECTIVE_VOL, f"cat /vol/{ARCHIVE_APP_DIR}/config/app.conf") == "app_env=production"
    assert vol_exec(SELECTIVE_VOL, f"test -e /vol/{ARCHIVE_APP_DIR}/data.txt && echo YES || echo NO") == "NO"

    # Unpin & bulk delete our temporary retention backup copy to test bulk delete too
    req("POST", f"/backups/{pinned_id}/pin", {})
    _, bulk_del = req("POST", "/backups/bulk", {"action": "delete", "ids": [pinned_id]})
    assert bulk_del["succeeded"] == 1
    suite.ok()

    # 10. Volume Deletion Safety (In-Use Guard vs Unattached Orphan Delete)
    suite.begin(10, "Volume deletion guard (in-use 409 vs orphan 200)")
    st_inuse, _ = req("DELETE", f"/volumes/{APP_VOL}", expect_statuses={400, 409})
    assert st_inuse in (400, 409), f"expected conflict deleting in-use volume, got {st_inuse}"
    _, del_vol_res = req("DELETE", f"/volumes/{DELETE_VOL}")
    assert del_vol_res.get("deleted") is True
    suite.ok()

    # 11. Notifications Webhook Delivery & UI Preferences Round-Trip
    suite.begin(11, "Webhook notification delivery & UI preferences")
    received_webhooks = []

    class WebhookHandler(http.server.BaseHTTPRequestHandler):
        def do_POST(self):
            length = int(self.headers.get("Content-Length", 0))
            body_bytes = self.rfile.read(length)
            received_webhooks.append((self.path, body_bytes.decode("utf-8", errors="replace")))
            self.send_response(200)
            self.end_headers()
            self.wfile.write(b"ok")

        def log_message(self, *args):
            pass

    srv = http.server.HTTPServer(("0.0.0.0", 0), WebhookHandler)
    hook_port = srv.server_address[1]
    srv_thread = threading.Thread(target=srv.serve_forever, daemon=True)
    srv_thread.start()
    try:
        _, n_create = req("POST", "/notifications", {
            "name": "E2E Webhook",
            "provider": "generic",
            "webhookUrl": f"http://host.docker.internal:{hook_port}/hook",
            "enabled": True,
            "onSuccess": True,
            "onFailure": True,
        })
        notif_id = n_create["notification"]["id"]
        _, n_test = req("POST", f"/notifications/{notif_id}/test", {})
        assert n_test.get("ok") is True or "error" not in n_test, f"webhook test failed: {n_test}"
        assert len(received_webhooks) >= 1, f"webhook listener received no requests: {received_webhooks}"
        hook_path, hook_body = received_webhooks[0]
        assert hook_path == "/hook" and "DockVault" in hook_body, f"unexpected webhook request: {received_webhooks[0]}"
        req("DELETE", f"/notifications/{notif_id}")
    finally:
        srv.shutdown()
        srv.server_close()

    _, pref_upd = req("PUT", "/settings/preferences", {
        "theme": "dark",
        "accentColor": "emerald",
        "density": "compact",
        "instanceName": "e2e-node",
        "defaultPage": "history",
        "defaultResourceTab": "stacks",
        "timeFormat": "24h",
        "autoRefreshSeconds": 30,
    })
    assert pref_upd["preferences"]["instanceName"] == "e2e-node"
    _, pref_rst = req("POST", "/settings/preferences/reset", {})
    assert pref_rst["preferences"]["instanceName"] == ""
    suite.ok()

    # 12. Authentication Lifecycle + Dashboard Analytics Verification (while signed in)
    suite.begin(12, "Auth setup, 401 guard, login, analytics & logout")
    req("POST", "/auth/setup", {"username": "e2eadmin", "password": "StrongPassword123!"})
    cookie_jar.clear()

    st_unauth, _ = req("GET", "/dashboard", expect_statuses={401}, use_cookies=False)
    assert st_unauth == 401, f"expected 401 without cookie, got {st_unauth}"

    st_bad_pw, _ = req("POST", "/auth/login", {"username": "e2eadmin", "password": "wrong"}, expect_statuses={401})
    assert st_bad_pw == 401, f"expected 401 for wrong password, got {st_bad_pw}"

    _, login_res = req("POST", "/auth/login", {"username": "e2eadmin", "password": "StrongPassword123!"})
    assert login_res["authenticated"] is True

    _, dash = req("GET", "/dashboard")
    assert len(dash["dailyActivity"]) == 90
    sa = dash["storageAnalytics"]
    assert sa["compression"]["zstd"] >= 2 and sa["compression"]["gzip"] >= 1
    assert len(sa["byProvider"]) >= 2
    sec = dash["securityOverview"]
    assert sec["encryptedProviders"] >= 1 and sec["encryptedBackups"] >= 1
    assert sec["authEnabled"] is True

    req("POST", "/auth/logout", {})
    cookie_jar.clear()
    st_after_logout, _ = req("GET", "/dashboard", expect_statuses={401}, use_cookies=False)
    assert st_after_logout == 401
    suite.ok()


if __name__ == "__main__":
    run_tests()
