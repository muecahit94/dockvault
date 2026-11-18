#!/usr/bin/env python3
"""Edge-case, chaos, security & cloud-storage E2E suite for DockVault."""

import http.cookiejar
import json
import os
import subprocess
import time
import urllib.error
import urllib.parse
import urllib.request

from e2e_ui import Suite

BASE = os.environ.get("DOCKVAULT_EDGE_E2E_URL", "http://127.0.0.1:18090/api").rstrip("/")
PREFIX = os.environ.get("DOCKVAULT_EDGE_E2E_PREFIX", "dv-edge-e2e")
WORKDIR = os.environ.get("DOCKVAULT_EDGE_E2E_WORKDIR", "/tmp")
SKIP_CLOUD = os.environ.get("DOCKVAULT_EDGE_E2E_SKIP_CLOUD", "0") == "1"

DOCKVAULT_C = f"{PREFIX}-dockvault"
TRICKY_C = f"{PREFIX}-tricky"
STOPPED_C = f"{PREFIX}-stopped"
MULTI_CHUNK_C = f"{PREFIX}-big"
AZURITE_C = f"{PREFIX}-azurite"
S3_C = f"{PREFIX}-s3"

TRICKY_V = f"{PREFIX}-tricky-vol"
EMPTY_V = f"{PREFIX}-empty-vol"
STOPPED_V = f"{PREFIX}-stopped-vol"
MULTI_CHUNK_V = f"{PREFIX}-big-vol"
GFS_V = f"{PREFIX}-gfs-vol"

AZURITE_KEY = "Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw=="

cookie_jar = http.cookiejar.CookieJar()
opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(cookie_jar))


def req(method, path, body=None, timeout=60, expect_statuses=None):
    url = BASE + path
    data = None
    headers = {}
    if body is not None:
        data = json.dumps(body).encode("utf-8")
        headers["Content-Type"] = "application/json"
    r = urllib.request.Request(url, data=data, headers=headers, method=method)
    try:
        with opener.open(r, timeout=timeout) as resp:
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


def wait_backup(backup_id, expect_status="success", timeout_sec=60):
    deadline = time.time() + timeout_sec
    last_job = None
    while time.time() < deadline:
        _, prog = req("GET", f"/backups/{backup_id}/progress")
        job = prog["job"]
        last_job = job
        if job["status"] in ("success", "failed"):
            assert job["status"] == expect_status, f"Backup {backup_id} status={job['status']}, wanted {expect_status}: {job}"
            _, detail = req("GET", "/backups?limit=100")
            for b in detail["backups"]:
                if b["id"] == backup_id:
                    return b, job
            return {"id": backup_id}, job
        time.sleep(0.35)
    raise TimeoutError(f"Backup {backup_id} timed out; last={last_job}")


def vol_exec(volume_name, sh_cmd):
    return subprocess.check_output(
        ["docker", "run", "--rm", "-v", f"{volume_name}:/vol", "alpine:3.21", "sh", "-c", sh_cmd],
        text=True,
    ).strip()


def run_tests():
    suite = Suite("Edge, Chaos & Cloud E2E Suite", 9)
    try:
        _run_edge_scenarios(suite)
    except Exception:
        suite.fail()
        raise
    suite.finish()


def _run_edge_scenarios(suite):
    _, storage_data = req("GET", "/storage")
    local_id = storage_data["providers"][0]["id"]
    _, cdata = req("GET", "/containers?all=true")
    containers = {c["name"]: c for c in cdata["containers"]}
    tricky = containers[TRICKY_C]
    stopped = containers[STOPPED_C]
    big = containers[MULTI_CHUNK_C]

    # 1. Pre-Hook Failure Recovery, Unpause Guarantee, Post-Hook Guarantee & Helper Cleanup
    suite.begin(1, "Pre-hook failure, post-hook & helper cleanup")
    _, fail_run = req("POST", "/backups/run", {
        "targetType": "container",
        "targetId": tricky["id"],
        "storageProviderId": local_id,
        "pauseContainer": True,
        "preCommand": "exit 42",
        "postCommand": "echo post-ran-after-failure > /data/post-fail.txt",
    })
    b_fail, _ = wait_backup(fail_run["backup"]["id"], expect_status="failed")
    assert "42" in b_fail.get("errorMessage", ""), f"expected exit 42 error message: {b_fail}"
    state = subprocess.check_output(["docker", "inspect", "-f", "{{.State.Status}}", TRICKY_C], text=True).strip()
    assert state == "running", f"container was left {state!r} after pre-hook failure!"
    post_marker = subprocess.check_output(["docker", "exec", TRICKY_C, "cat", "/data/post-fail.txt"], text=True).strip()
    assert post_marker == "post-ran-after-failure", f"postCommand did not run after preCommand failure: {post_marker!r}"
    helpers = subprocess.check_output(["docker", "ps", "-aq", "--filter", "label=dockvault.helper=true"], text=True).strip()
    assert helpers == "", f"stray helper containers left behind: {helpers}"
    suite.ok()

    # 2. Per-Target Concurrency Lock (409 Conflict)
    suite.begin(2, "Per-target concurrency lock (409 Conflict)")
    _, slow_run = req("POST", "/backups/run", {
        "targetType": "container",
        "targetId": tricky["id"],
        "storageProviderId": local_id,
        "pauseContainer": False,
        "preCommand": "sleep 2",
    })
    slow_id = slow_run["backup"]["id"]
    st_conflict, conflict_body = req("POST", "/backups/run", {
        "targetType": "container",
        "targetId": tricky["id"],
        "storageProviderId": local_id,
    }, expect_statuses={409})
    assert st_conflict == 409, f"expected 409 Conflict on concurrent backup, got {st_conflict}: {conflict_body}"
    wait_backup(slow_id, expect_status="success")
    suite.ok()

    # 3. Corrupted Multi-Chunk Encrypted Archive Detection & Restore Volume Safety
    suite.begin(3, "Corrupted multi-chunk AEAD restore volume guard")
    _, enc_prov = req("POST", "/storage", {
        "name": f"{PREFIX} Encrypted",
        "type": "local",
        "localPath": "/app/backups/enc-edge",
        "encryptBackups": True,
        "encryptionPassphrase": "key-alpha-strong",
    })
    enc_id = enc_prov["provider"]["id"]

    _, big_run = req("POST", "/backups/run", {
        "targetType": "container",
        "targetId": big["id"],
        "storageProviderId": enc_id,
        "compression": "zstd",
        "pauseContainer": False,
    })
    b_big, _ = wait_backup(big_run["backup"]["id"])
    assert b_big["sizeBytes"] > 131072, f"expected >128KB (>=3 AEAD chunks) to test mid-stream corruption, got {b_big['sizeBytes']}"

    # Corrupt byte at offset 90000 (chunk 2 of DVENC1, AFTER header and chunk 1!)
    enc_host_path = os.path.join(WORKDIR, "backups", "enc-edge", b_big["fileName"])
    with open(enc_host_path, "r+b") as f:
        f.seek(90000)
        b = f.read(1)
        f.seek(90000)
        f.write(bytes([b[0] ^ 0xFF]))

    _, ver_corrupt = req("POST", f"/backups/{b_big['id']}/verify", {}, expect_statuses={200, 400})
    assert ver_corrupt.get("ok") is False or ver_corrupt.get("match") is False or "error" in ver_corrupt, f"corruption went undetected: {ver_corrupt}"

    # Seed canary in live volume and attempt restore of tampered archive -> MUST abort WITHOUT wiping volume!
    vol_exec(MULTI_CHUNK_V, "echo 'canary=must-survive' > /vol/canary.txt")
    st_corrupt_rst, _ = req("POST", f"/backups/{b_big['id']}/restore", {"pauseContainer": False}, expect_statuses={400, 500})
    assert st_corrupt_rst in (400, 500), f"expected tampered archive restore to fail, got {st_corrupt_rst}"
    canary_after = vol_exec(MULTI_CHUNK_V, "cat /vol/canary.txt")
    assert canary_after == "canary=must-survive", f"BUG: tampered restore wiped live volume canary! got={canary_after!r}"
    suite.ok()

    # 4. Wrong Passphrase Recovery Safety
    suite.begin(4, "Wrong passphrase rejection across API endpoints")
    req("PUT", f"/storage/{enc_id}", {
        "name": f"{PREFIX} Encrypted",
        "type": "local",
        "localPath": "/app/backups/enc-edge",
        "encryptBackups": True,
        "encryptionPassphrase": "wrong-passphrase-beta",
    })
    st_bad_insp, _ = req("GET", f"/backups/{b_big['id']}/inspect", expect_statuses={400})
    st_bad_dl, _ = req("GET", f"/backups/{b_big['id']}/download", expect_statuses={400})
    st_bad_rst, _ = req("POST", f"/backups/{b_big['id']}/restore", {"pauseContainer": False}, expect_statuses={400})
    assert st_bad_insp == 400 and st_bad_dl == 400 and st_bad_rst == 400, (
        f"expected 400s on wrong passphrase: inspect={st_bad_insp}, dl={st_bad_dl}, rst={st_bad_rst}"
    )
    assert vol_exec(MULTI_CHUNK_V, "cat /vol/canary.txt") == "canary=must-survive"
    suite.ok()

    # 5. Tricky Filenames, Symlinks, Dotfiles, 0-byte Files, Empty Volume, & Path Traversal Security
    suite.begin(5, "Tricky filenames, symlinks, empty-vol & path guards")
    _, tricky_run = req("POST", "/backups/run", {
        "targetType": "container",
        "targetId": tricky["id"],
        "storageProviderId": local_id,
        "compression": "zstd",
        "excludePatterns": "*.tmp",
        "pauseContainer": True,
    })
    b_tricky, _ = wait_backup(tricky_run["backup"]["id"])
    _, t_insp = req("GET", f"/backups/{b_tricky['id']}/inspect")
    t_names = [e["name"] for e in t_insp["entries"]]
    assert not any("ignored space.tmp" in n for n in t_names), f"glob with space should have been excluded: {t_names}"

    space_entry = next(n for n in t_names if n.endswith("file with spaces.txt"))
    utf8_entry = next(n for n in t_names if "ümlaut" in n)
    quote_entry = next(n for n in t_names if "ampersand.txt" in n)

    _, sp_bytes = req("GET", f"/backups/{b_tricky['id']}/file?path={urllib.parse.quote(space_entry)}")
    assert sp_bytes.decode("utf-8").strip() == "space-ok"
    _, u8_bytes = req("GET", f"/backups/{b_tricky['id']}/file?path={urllib.parse.quote(utf8_entry)}")
    assert u8_bytes.decode("utf-8").strip() == "utf8-euro-ok"
    _, qt_bytes = req("GET", f"/backups/{b_tricky['id']}/file?path={urllib.parse.quote(quote_entry)}")
    assert qt_bytes.decode("utf-8").strip() == "quote-amp-ok"

    # Traversal & non-existent selective path guards
    st_trav, _ = req("GET", f"/backups/{b_tricky['id']}/file?path=../../etc/passwd", expect_statuses={400, 404})
    assert st_trav in (400, 404)
    st_nomatch, _ = req("POST", f"/backups/{b_tricky['id']}/restore", {
        "pauseContainer": False,
        "paths": ["does/not/exist.txt"],
    }, expect_statuses={400})
    assert st_nomatch == 400, f"expected 400 for non-matching selective restore, got {st_nomatch}"

    # Wipe tricky volume completely (adding extra garbage) and run full restore -> verify everything returns identically
    vol_exec(TRICKY_V, "find /vol -mindepth 1 -maxdepth 1 -exec rm -rf {} + && echo garbage > /vol/stale.txt")
    req("POST", f"/backups/{b_tricky['id']}/restore", {"pauseContainer": True})
    check_script = """
      set -e
      test "$(cat '/vol/file with spaces.txt')" = "space-ok"
      test "$(cat '/vol/ümlaut-€-file.txt')" = "utf8-euro-ok"
      test "$(cat "/vol/quote's & ampersand.txt")" = "quote-amp-ok"
      test "$(cat '/vol/dir with spaces/inner file.conf')" = "nested-space"
      test "$(cat /vol/.env.secret)" = "SECRET=top"
      test "$(cat /vol/.hidden/token)" = "hidden-nested"
      test -f /vol/empty.zero && test ! -s /vol/empty.zero
      test -L /vol/sym-link
      test ! -e /vol/stale.txt
      echo ALL_TRICKY_OK
    """
    assert vol_exec(TRICKY_V, check_script) == "ALL_TRICKY_OK"

    # Empty volume backup + restore
    _, emp_run = req("POST", "/backups/run", {
        "targetType": "volume",
        "targetId": EMPTY_V,
        "storageProviderId": local_id,
        "compression": "zstd",
    })
    b_emp, _ = wait_backup(emp_run["backup"]["id"])
    req("POST", f"/backups/{b_emp['id']}/verify", {})
    vol_exec(EMPTY_V, "echo should-be-wiped > /vol/extra.txt")
    req("POST", f"/backups/{b_emp['id']}/restore", {"pauseContainer": False})
    assert vol_exec(EMPTY_V, "find /vol -mindepth 1") == "", "empty volume restore left leftover files"
    suite.ok()

    # 6. Stopped Container Backup & Restore While Stopped
    suite.begin(6, "Stopped container backup & restore")
    assert stopped["state"] in ("created", "exited"), f"expected non-running stopped container: {stopped}"
    _, stp_run = req("POST", "/backups/run", {
        "targetType": "container",
        "targetId": stopped["id"],
        "storageProviderId": local_id,
        "pauseContainer": True,
        "compression": "gzip",
    })
    b_stp, _ = wait_backup(stp_run["backup"]["id"])
    vol_exec(STOPPED_V, "echo ruined > /vol/state.txt")
    req("POST", f"/backups/{b_stp['id']}/restore", {"pauseContainer": True})
    assert vol_exec(STOPPED_V, "cat /vol/state.txt") == "stopped-baseline"
    suite.ok()

    # 7 & 8. Real Cloud Object Storage E2E: Azurite (Azure Blob) & SeaweedFS (S3)
    if not SKIP_CLOUD:
        suite.begin(7, "Azure Blob (Azurite) .tar.zst.enc, SAS & DR restore")
        az_payload = {
            "name": f"{PREFIX} Azurite",
            "type": "azure",
            "azureAuthType": "key",
            "azureAccountName": "devstoreaccount1",
            "secretKey": AZURITE_KEY,
            "endpoint": f"http://{AZURITE_C}:10000/devstoreaccount1",
            "azureContainer": "dockvault-e2e",
            "prefix": "cluster-a",
            "azureAccessTier": "Hot",
            "azureAutoCreateContainer": True,
            "encryptBackups": True,
            "encryptionPassphrase": "azure-vault-passphrase",
        }
        _, az_test = req("POST", "/storage/test", az_payload)
        assert az_test["ok"] is True, f"Azurite test failed: {az_test}"
        _, az_prov = req("POST", "/storage", az_payload)
        az_id = az_prov["provider"]["id"]

        _, az_run = req("POST", "/backups/run", {
            "targetType": "volume",
            "targetId": STOPPED_V,
            "storageProviderId": az_id,
            "compression": "zstd",
        })
        b_az, _ = wait_backup(az_run["backup"]["id"])
        assert b_az["encrypted"] is True and b_az["fileName"].endswith(".tar.zst.enc")
        _, az_ver = req("POST", f"/backups/{b_az['id']}/verify", {})
        assert az_ver["match"] is True
        _, az_insp = req("GET", f"/backups/{b_az['id']}/inspect")
        assert any(e["name"].endswith("state.txt") for e in az_insp["entries"])
        _, sas_res = req("POST", f"/backups/{b_az['id']}/sas", {})
        assert sas_res.get("url") and "sig=" in sas_res["url"], f"unexpected SAS response: {sas_res}"

        # Scan & import from Azure Blob
        req("DELETE", f"/backups/{b_az['id']}?deleteFile=false")
        _, az_scan = req("POST", f"/storage/{az_id}/scan", {})
        assert any(o["storagePath"] == b_az["storagePath"] and not o["alreadyImported"] for o in az_scan["objects"])
        _, az_imp = req("POST", f"/storage/{az_id}/import", {"storagePaths": [b_az["storagePath"]]})
        assert az_imp["imported"] == 1

        # Find imported backup ID & restore + cleanup remote blob
        _, az_list = req("GET", f"/backups?target={urllib.parse.quote(STOPPED_V)}&limit=10")
        imp_az = next(b for b in az_list["backups"] if b["fileName"] == b_az["fileName"])
        vol_exec(STOPPED_V, "echo wiped-by-disaster > /vol/state.txt")
        req("POST", f"/backups/{imp_az['id']}/restore", {"pauseContainer": False})
        assert vol_exec(STOPPED_V, "cat /vol/state.txt") == "stopped-baseline"
        req("DELETE", f"/backups/{imp_az['id']}?deleteFile=true")
        suite.ok()

        suite.begin(8, "S3 (SeaweedFS) .tar.zst.enc, verify & DR restore")
        s3_payload = {
            "name": f"{PREFIX} SeaweedFS S3",
            "type": "s3",
            "endpoint": f"http://{S3_C}:8333",
            "region": "us-east-1",
            "bucket": "dockvault-bucket",
            "prefix": "prod-s3",
            "accessKey": "e2eaccesskey",
            "secretKey": "e2esecretkey123",
            "useSSL": False,
            "encryptBackups": True,
            "encryptionPassphrase": "s3-vault-passphrase",
        }
        # SeaweedFS auto-creates bucket on upload or bucket test; check bucket creation via PUT or /storage/test
        st_s3_create_test, _ = req("POST", "/storage/test", s3_payload, expect_statuses={200, 400})
        if st_s3_create_test == 400:
            # Create bucket on SeaweedFS S3 using minimal SigV4 or s3cmd? SeaweedFS filer/S3 supports anonymous bucket PUT only when no identity; upload auto-creates bucket with -s3.autoCreateBucket!
            pass
        _, s3_prov = req("POST", "/storage", s3_payload)
        s3_id = s3_prov["provider"]["id"]

        _, s3_run = req("POST", "/backups/run", {
            "targetType": "volume",
            "targetId": STOPPED_V,
            "storageProviderId": s3_id,
            "compression": "zstd",
        })
        b_s3, _ = wait_backup(s3_run["backup"]["id"])
        assert b_s3["encrypted"] is True and b_s3["fileName"].endswith(".tar.zst.enc")
        _, s3_test_after = req("POST", f"/storage/{s3_id}/test", {})
        assert s3_test_after["ok"] is True
        _, s3_ver = req("POST", f"/backups/{b_s3['id']}/verify", {})
        assert s3_ver["match"] is True
        _, s3_insp = req("GET", f"/backups/{b_s3['id']}/inspect")
        assert any(e["name"].endswith("state.txt") for e in s3_insp["entries"])

        req("DELETE", f"/backups/{b_s3['id']}?deleteFile=false")
        _, s3_scan = req("POST", f"/storage/{s3_id}/scan", {})
        assert any(o["storagePath"] == b_s3["storagePath"] and not o["alreadyImported"] for o in s3_scan["objects"])
        _, s3_imp = req("POST", f"/storage/{s3_id}/import", {"storagePaths": [b_s3["storagePath"]]})
        assert s3_imp["imported"] == 1

        _, s3_list = req("GET", f"/backups?target={urllib.parse.quote(STOPPED_V)}&limit=10")
        imp_s3 = next(b for b in s3_list["backups"] if b["fileName"] == b_s3["fileName"])
        vol_exec(STOPPED_V, "echo wiped-s3-disaster > /vol/state.txt")
        req("POST", f"/backups/{imp_s3['id']}/restore", {"pauseContainer": False})
        assert vol_exec(STOPPED_V, "cat /vol/state.txt") == "stopped-baseline"
        req("DELETE", f"/backups/{imp_s3['id']}?deleteFile=true")
        suite.ok()
    else:
        suite.begin(7, "Azure Blob (Azurite) cloud storage")
        suite.ok("skipped (--skip-cloud)")
        suite.begin(8, "S3 (SeaweedFS) object storage")
        suite.ok("skipped (--skip-cloud)")

    # 9. GFS Retention + Login Brute-Force Rate Limiter (429)
    suite.begin(9, "GFS retention pruning & login rate limiter (429)")
    _, gfs_sch = req("POST", "/schedules", {
        "name": f"{PREFIX} GFS Keep-1",
        "targetType": "volume",
        "targetId": GFS_V,
        "targetName": GFS_V,
        "cronExpr": "0 6 * * *",
        "storageProviderId": local_id,
        "pauseContainer": False,
        "retentionPolicy": "gfs",
        "keepDaily": 1,
        "keepWeekly": 0,
        "keepMonthly": 0,
        "compression": "zstd",
        "enabled": True,
    })
    gfs_id = gfs_sch["schedule"]["id"]
    seen_gfs_ids = set()
    for idx in range(3):
        if idx > 0:
            time.sleep(1.1)
        req("POST", f"/schedules/{gfs_id}/run", {})
        deadline = time.time() + 30
        while time.time() < deadline:
            _, res = req("GET", f"/backups?target={urllib.parse.quote(GFS_V)}&limit=10")
            running = any(b["status"] == "running" for b in res["backups"])
            newer = [b for b in res["backups"] if b["id"] not in seen_gfs_ids and b["status"] == "success"]
            if not running and newer:
                seen_gfs_ids |= {b["id"] for b in res["backups"]}
                break
            time.sleep(0.3)
        else:
            raise TimeoutError(f"GFS schedule run {idx+1} timed out")
    _, gfs_final = req("GET", f"/backups?target={urllib.parse.quote(GFS_V)}&limit=10")
    assert len(gfs_final["backups"]) == 1, f"expected GFS keepDaily=1 to prune down to 1 backup, got {len(gfs_final['backups'])}: {gfs_final}"
    req("DELETE", f"/schedules/{gfs_id}")

    # Auth setup + 10 failed logins -> 11th returns 429; isolated per username
    req("POST", "/auth/setup", {"username": "ratelimitadmin", "password": "ValidPassword123!"})
    cookie_jar.clear()
    for i in range(10):
        st_l, _ = req("POST", "/auth/login", {"username": "ratelimitadmin", "password": f"wrong-{i}"}, expect_statuses={401})
        assert st_l == 401
    st_rl, _ = req("POST", "/auth/login", {"username": "ratelimitadmin", "password": "wrong-11"}, expect_statuses={429})
    assert st_rl == 429, f"expected 429 after 10 failed logins, got {st_rl}"
    st_other, _ = req("POST", "/auth/login", {"username": "otheruser", "password": "wrong"}, expect_statuses={401})
    assert st_other == 401, f"other username should not be locked out, got {st_other}"
    suite.ok()


if __name__ == "__main__":
    run_tests()
