#!/usr/bin/env python3
"""CI-only real Docker carrier lifecycle; isolated synthetic database, no Target.

CLI/domain correctness uses the real migrated Go integration tests. This fixture
exercises the actual Compose/exec/cleanup boundary and a lost host response.
"""
import argparse
import importlib.util
import json
import os
import platform
from pathlib import Path
import subprocess
import tempfile
import time
import uuid
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("rollout", Path(__file__).with_name("internal-target-rollout.py"))
r = importlib.util.module_from_spec(spec)
spec.loader.exec_module(r)



def carrier_read_ledgers(migrations):
    versions=sorted(p.name[:-7] for p in migrations.glob("*.up.sql"))
    expected=["000117_execution_observations","000118_source_budget_snapshots"]
    if versions[-2:] != expected:
        raise RuntimeError("carrier read fixture requires exact M117/M118 artifact heads")
    return [("bridge",versions[:-1]),("current",versions)]


def carrier_compose(image, repo):
    return {"services": {
        "db": {"image":"postgres:16-alpine", "environment":{"POSTGRES_PASSWORD":"owner-fixture-only"},
               "tmpfs":["/var/lib/postgresql/data"]},
        "backend": {"image":image, "command":["./uniroute"], "restart":"always", "stop_grace_period":"35m",
            "logging":{"driver":"local","options":{"max-size":"10m","max-file":"3"}},
            "ports":[{"target":8080,"published":"18080","host_ip":"127.0.0.1"}],
            "volumes":[{"type":"bind","source":str(repo / "migrations"),"target":"/app/migrations","read_only":True}],
            "environment":{"RUNTIME_ENVIRONMENT":"test", "DB_DSN":"postgres://postgres:owner-fixture-only@db:5432/postgres?sslmode=disable",
                "JWT_SECRET":"ci-jwt-secret-0123456789abcdef0123456789abcdef",
                "API_KEY_HASH_SECRET":"ci-api-key-hash-0123456789abcdef0123456789abcdef",
                "ENCRYPTION_KEY":"0123456789abcdef0123456789abcdef",
                "UNIROUTE_NOTIFICATION_WEBHOOK_SIGNING_SECRET":"0123456789abcdef0123456789abcdef"}}}}


def main():
    if os.environ.get("CI") != "true":
        raise RuntimeError("this isolated Docker fixture is CI-only")
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--image", required=True)
    args = parser.parse_args()
    image = r.load(r.run(["docker", "image", "inspect", args.image]))[0]
    project = "ur-owner-ci-" + uuid.uuid4().hex[:10]
    repo = Path(__file__).resolve().parent.parent
    with tempfile.TemporaryDirectory(prefix="unirouter-owner-ci-") as tmp:
        root = Path(tmp).resolve()
        env = root / "fixture.env"
        env.write_text("")
        base = carrier_compose(args.image, repo)
        paths = [root / ("compose-"+str(i)+".json") for i in range(5)]
        for i,p in enumerate(paths): p.write_text(json.dumps(base if i==0 else {"services":{}}))
        compose = ["docker","compose","--project-name",project,"--env-file",str(env)]
        for p in paths: compose += ["-f",str(p)]
        r.run(["docker","pull","postgres:16-alpine"],timeout=120)
        try:
            r.run(compose+["up","-d","--no-deps","db"],timeout=60)
            pg = r.run(compose+["ps","-q","db"]).decode().strip()
            deadline = time.monotonic()+30
            while True:
                # The image's initialization server exposes only a Unix socket.
                # Wait for the final TCP server before installing the fixture.
                ready=subprocess.run(["docker","exec",pg,"pg_isready","-h","127.0.0.1","-U","postgres"],capture_output=True)
                if ready.returncode==0: break
                if time.monotonic()>deadline: raise RuntimeError("isolated fixture database did not start")
                time.sleep(.2)
            ledgers=carrier_read_ledgers(repo/"migrations")
            # Only the read fixture's tables are created; not a fake installation
            # claim. The migrated Go suite separately proves the full DB states.
            sql="""CREATE TABLE schema_migrations(version text);
CREATE TABLE runtime_ingress_changes(change_id text, predecessor_id text, status text);
CREATE TABLE runtime_support_manifests(artifact_digest text, manifest_digest text);
CREATE TABLE runtime_generations(instance_id text, artifact_digest text, schema_head text, state text, ingress_state text, lease_expires_at timestamptz);
"""
            r.run(["docker","exec","-i",pg,"psql","-U","postgres","-v","ON_ERROR_STOP=1"],data=sql.encode())
            source={"directory":str(repo),"version":"1.0.0-ci","commit":"a"*40,"tree":"b"*40}
            desired={"tag":args.image,"digest":image["Id"]}
            document={"runtime_owner":"artifact_cli","operation_id":project,"target":{"project":project,"env_file":str(env),"overlay":str(paths[-1]),"environment":"test"},
                      "source":source,"images":{"backend":desired,"frontend":desired},"evidence_directory":str(root/"evidence")}
            owner=r.Rollout(document)
            # This fixture has no real host/Target identity. The normal owner
            # checks remain covered by the formal plan tests and Target plan.
            owner.identity=lambda:None
            owner.images=lambda:{"backend":image}
            owner.sources=lambda:None
            owner.chain=lambda *a,**k:[str(p) for p in paths]
            fd=os.open(root/"fixture.lock",os.O_RDWR|os.O_CREAT,0o600)
            r.HELD_LOCK_FDS.append(fd)
            try:
                reports=[]
                name="unirouter-owner-"+r.digest(r.canonical(document))[:24]
                readback="SELECT jsonb_build_object('ledger',(SELECT jsonb_agg(version ORDER BY version) FROM schema_migrations),'ingress',(SELECT count(*) FROM runtime_ingress_changes),'manifests',(SELECT count(*) FROM runtime_support_manifests),'generations',(SELECT count(*) FROM runtime_generations))::text;"
                def state():
                    return r.load(r.run(["docker","exec",pg,"psql","-U","postgres","-Atc",readback]))
                for label,versions in ledgers:
                    sql="BEGIN; TRUNCATE schema_migrations; INSERT INTO schema_migrations VALUES "+",".join("('"+v+"')" for v in versions)+"; COMMIT;"
                    r.run(["docker","exec","-i",pg,"psql","-U","postgres","-v","ON_ERROR_STOP=1"],data=sql.encode())
                    before=state()
                    assert before=={"ledger":versions,"ingress":0,"manifests":0,"generations":0},before
                    result=owner.artifact_owner_command("ignored","inspect")
                    assert result=={"head":"","head_status":"","generations":[]},result
                    assert state()==before,"inspect changed the read fixture"
                    assert r.inspect(name,missing_ok=True) is None
                    reports.append({"role":label,"head":versions[-1],"ledger_count":len(versions),"ledger_sha256":r.digest(r.canonical(versions)),"inspect":"passed","unchanged":"passed"})
                # Lose the current-head command response after actual completion.
                # The same finally path must still stop the exact real carrier.
                original=subprocess.run
                def lost(args,**kw):
                    result=original(args,**kw)
                    if args[:3]==["docker","exec","-i"] and "runtime-generation" in args:
                        raise subprocess.TimeoutExpired(["docker","exec","<exact-owned-id>"],30)
                    return result
                try:
                    with patch.object(subprocess,"run",side_effect=lost):
                        owner.artifact_owner_command("ignored","inspect")
                    raise AssertionError("lost response must not be successful")
                except subprocess.TimeoutExpired:
                    pass
                assert r.inspect(name,missing_ok=True) is None
                assert state()==before,"lost response changed the read fixture"
                docker_version=r.run(["docker","version","--format","{{.Client.Version}} / {{.Server.Version}}"]).decode().strip()
                print(json.dumps({"docker_carrier":"passed","docker_client_server":docker_version,"python_version":platform.python_version(),"tested_ledgers":reports,"fixture_schema_scope":"four read tables only; not a full migration or installation","lost_response_cleanup":"passed","ingress_writes":0,"provider_calls":0}))
            finally:
                r.HELD_LOCK_FDS.clear();os.close(fd)
        finally:
            # Only the fresh uniquely named fixture's Compose resources.
            r.run(compose+["down","--volumes"],timeout=60)


if __name__ == "__main__":
    main()
