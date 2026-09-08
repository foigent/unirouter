#!/usr/bin/env python3
"""CI-only real Docker carrier lifecycle; isolated synthetic database, no Target.

CLI/domain correctness uses the real migrated Go integration tests. This fixture
exercises the actual Compose/exec/cleanup boundary and a lost host response.
"""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import tempfile
import time
import uuid
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("rollout", Path(__file__).with_name("internal-target-rollout.py"))
r = importlib.util.module_from_spec(spec)
spec.loader.exec_module(r)


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
        base = {"services": {
            "db": {"image":"postgres:16-alpine", "environment":{"POSTGRES_PASSWORD":"owner-fixture-only"},
                   "tmpfs":["/var/lib/postgresql/data"]},
            "backend": {"image":args.image, "command":["./uniroute"], "restart":"always", "stop_grace_period":"35m",
                "ports":[{"target":8080,"published":"18080","host_ip":"127.0.0.1"}],
                "volumes":[{"type":"bind","source":str(repo / "migrations"),"target":"/app/migrations","read_only":True}],
                "environment":{"RUNTIME_ENVIRONMENT":"test", "DB_DSN":"postgres://postgres:owner-fixture-only@db:5432/postgres?sslmode=disable",
                    "JWT_SECRET":"ci-jwt-secret-0123456789abcdef0123456789abcdef",
                    "API_KEY_HASH_SECRET":"ci-api-key-hash-0123456789abcdef0123456789abcdef",
                    "ENCRYPTION_KEY":"0123456789abcdef0123456789abcdef",
                    "UNIROUTE_NOTIFICATION_WEBHOOK_SIGNING_SECRET":"0123456789abcdef0123456789abcdef"}}}}
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
                ready=subprocess.run(["docker","exec",pg,"pg_isready","-U","postgres"],capture_output=True)
                if ready.returncode==0: break
                if time.monotonic()>deadline: raise RuntimeError("isolated fixture database did not start")
                time.sleep(.2)
            versions=sorted(p.name[:-7] for p in (repo/"migrations").glob("*.up.sql"))
            # Only the read fixture's tables are created; not a fake installation
            # claim. The migrated Go suite separately proves the full DB states.
            sql="""CREATE TABLE schema_migrations(version text);
CREATE TABLE runtime_ingress_changes(change_id text, predecessor_id text, status text);
CREATE TABLE runtime_support_manifests(artifact_digest text, manifest_digest text);
CREATE TABLE runtime_generations(instance_id text, artifact_digest text, schema_head text, state text, ingress_state text, lease_expires_at timestamptz);
"""+"INSERT INTO schema_migrations VALUES "+",".join("('"+v+"')" for v in versions)+";"
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
                result=owner.artifact_owner_command("ignored","inspect")
                assert result=={"head":"","head_status":"","generations":[]},result
                name="unirouter-owner-"+r.digest(r.canonical(document))[:24]
                assert r.inspect(name,missing_ok=True) is None
                # The actual command completed, but its host response is lost.
                # The same finally path must still stop the real exact carrier.
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
                row=r.run(["docker","exec",pg,"psql","-U","postgres","-Atc","SELECT count(*) FROM runtime_ingress_changes"]).strip()
                assert row==b"0"
                docker_version=r.run(["docker","version","--format","{{.Client.Version}} / {{.Server.Version}}"]).decode().strip()
                print(json.dumps({"docker_carrier":"passed","docker_client_server":docker_version,"cli_head":"M115","lost_response_cleanup":"passed","ingress_writes":0,"provider_calls":0}))
            finally:
                r.HELD_LOCK_FDS.clear();os.close(fd)
        finally:
            # Only the fresh uniquely named fixture's Compose resources.
            r.run(compose+["down","--volumes"],timeout=60)


if __name__ == "__main__":
    main()
