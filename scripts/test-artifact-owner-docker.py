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



def carrier_read_ledgers(migrations, historical=False):
    versions=sorted(p.name[:-7] for p in migrations.glob("*.up.sql"))
    expected=(["000117_execution_observations","000118_source_budget_snapshots"] if historical else
              list(r.BRIDGE_HEADS[r.BRIDGE_HEADS.index(r.XINSIGHT_POLICY_HEAD):]) +
              ["000126_commercial_license_retirement", "000127_xinsight_communication_policy",
               "000128_upstream_release_observation", "000129_upstream_observation_restore_namespace"])
    if versions[-len(expected):] != expected:
        raise RuntimeError("carrier read fixture requires exact reviewed artifact heads")
    return [("bridge" if historical else "maintenance_predecessor",versions[:-1]),("current",versions)]


def carrier_read_namespace_sql(migrations):
    # Copy the original predicates and the existing M129 correction, rather
    # than claiming a current ledger over a database missing its read contract.
    source=(migrations/"000128_upstream_release_observation.up.sql").read_text()
    begin="CREATE FUNCTION valid_upstream_release_text("
    end="\nALTER TABLE model_deployments\n"
    if source.count(begin)!=1 or source.count(end)!=1:
        raise RuntimeError("carrier read fixture requires exact validator definitions")
    functions=begin+source.split(begin,1)[1].split(end,1)[0]
    if r.digest(functions.encode())!="934220eb5e26f57110186440404abfcb1958f890f85d571bbf5327c49ebefa1e":
        raise RuntimeError("carrier read fixture validator definitions changed")
    source=(migrations/"000129_upstream_observation_restore_namespace.up.sql").read_text()
    begin="-- unirouter:observation-namespace-correction-begin\n"
    end="-- unirouter:observation-namespace-correction-end"
    if source.count(begin)!=1 or source.count(end)!=1:
        raise RuntimeError("carrier read fixture requires exact namespace correction")
    correction=source.split(begin,1)[1].split(end,1)[0].strip()
    if correction!="ALTER FUNCTION public.valid_upstream_release_observation(jsonb) SET search_path TO pg_catalog, public;":
        raise RuntimeError("carrier read fixture namespace correction changed")
    return functions,correction


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
            # Only the read tables and exact namespace predicates are created;
            # the migrated Go suite separately proves the full DB states.
            sql="""CREATE TABLE schema_migrations(version text);
CREATE TABLE runtime_ingress_changes(change_id text, predecessor_id text, status text);
CREATE TABLE runtime_support_manifests(artifact_digest text, manifest_digest text);
CREATE TABLE runtime_generations(instance_id text, artifact_digest text, compatibility_epoch text, schema_head text, revision bigint, state text, ingress_state text, lease_expires_at timestamptz);
"""
            functions,correction=carrier_read_namespace_sql(repo/"migrations")
            sql+=functions
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
                def namespace():
                    query="SELECT jsonb_agg(jsonb_build_object('name',p.proname,'body_sha256',encode(sha256(convert_to(p.prosrc,'UTF8')),'hex'),'config',p.proconfig) ORDER BY p.proname)::text FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='public' AND p.proname IN ('valid_upstream_release_text','valid_upstream_release_observation');"
                    return r.load(r.run(["docker","exec",pg,"psql","-U","postgres","-Atc",query]))
                for label,versions in ledgers:
                    sql="BEGIN; TRUNCATE schema_migrations; INSERT INTO schema_migrations VALUES "+",".join("('"+v+"')" for v in versions)+"; COMMIT;"
                    r.run(["docker","exec","-i",pg,"psql","-U","postgres","-v","ON_ERROR_STOP=1"],data=sql.encode())
                    before=state()
                    assert before=={"ledger":versions,"ingress":0,"manifests":0,"generations":0},before
                    if label=="current":
                        invalid_namespace=namespace()
                        assert all(row["config"] is None for row in invalid_namespace)
                        try:
                            owner.artifact_owner_command("ignored","inspect")
                            raise AssertionError("M129 must refuse its uncorrected validator namespace")
                        except RuntimeError as exc:
                            assert str(exc).startswith("Artifact owner refused inspect"),str(exc)
                        assert state()==before and namespace()==invalid_namespace,"refused inspect changed the read fixture"
                        assert r.inspect(name,missing_ok=True) is None
                        r.run(["docker","exec","-i",pg,"psql","-U","postgres","-v","ON_ERROR_STOP=1"],data=correction.encode())
                    namespace_before=namespace()
                    expected_config=["search_path=pg_catalog, public"] if label=="current" else None
                    assert namespace_before[0]["name"]=="valid_upstream_release_observation"
                    assert namespace_before[0]["config"]==expected_config
                    result=owner.artifact_owner_command("ignored","inspect")
                    assert result=={"head":"","head_status":"","generations":[]},result
                    assert state()==before,"inspect changed the read fixture"
                    assert namespace()==namespace_before,"inspect changed the validator namespace"
                    assert r.inspect(name,missing_ok=True) is None
                    reports.append({"role":label,"head":versions[-1],"ledger_count":len(versions),"ledger_sha256":r.digest(r.canonical(versions)),"inspect":"passed","unchanged":"passed","namespace_config":expected_config,"uncorrected_namespace_refused":label=="current"})
                fixture_artifact="sha256:"+"a"*64
                fixture_manifest="b"*64
                fixture_instance="uri_"+"c"*32
                fixture_epoch="unirouter.runtime-compatibility.v1"
                fixture_revision=7
                sql=("INSERT INTO runtime_support_manifests VALUES ('"+fixture_artifact+"','"+fixture_manifest+"');"
                     "INSERT INTO runtime_generations VALUES ('"+fixture_instance+"','"+fixture_artifact+"','"+fixture_epoch+"','"+versions[-1]+"',"+str(fixture_revision)+",'draining','absent','2000-01-01 UTC');")
                r.run(["docker","exec","-i",pg,"psql","-U","postgres","-v","ON_ERROR_STOP=1"],data=sql.encode())
                with_row=state()
                assert with_row=={"ledger":versions,"ingress":0,"manifests":1,"generations":1},with_row
                result=owner.artifact_owner_command("ignored","inspect")
                assert result=={"head":"","head_status":"","generations":[{"instance":fixture_instance,
                    "artifact_digest":fixture_artifact,"compatibility_epoch":fixture_epoch,"schema_head":versions[-1],
                    "revision":fixture_revision,"state":"draining","ingress_state":"absent","lease_live":False,
                    "support_manifest_digest":fixture_manifest}]},result
                assert state()==with_row,"inspect changed the populated read fixture"
                assert namespace()==namespace_before,"populated inspect changed the validator namespace"
                reports[-1]["inspect_revision"]="passed"
                r.run(["docker","exec","-i",pg,"psql","-U","postgres","-v","ON_ERROR_STOP=1"],
                      data=b"TRUNCATE runtime_generations, runtime_support_manifests;")
                assert state()==before,"populated read fixture cleanup failed"
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
                assert namespace()==namespace_before,"lost response changed the validator namespace"
                docker_version=r.run(["docker","version","--format","{{.Client.Version}} / {{.Server.Version}}"]).decode().strip()
                print(json.dumps({"docker_carrier":"passed","docker_client_server":docker_version,"python_version":platform.python_version(),"tested_ledgers":reports,"fixture_schema_scope":"four read tables and exact M128/M129 validator DDL only; not a full migration or installation","lost_response_cleanup":"passed","ingress_writes":0,"provider_calls":0}))
            finally:
                r.HELD_LOCK_FDS.clear();os.close(fd)
        finally:
            # Only the fresh uniquely named fixture's Compose resources.
            r.run(compose+["down","--volumes"],timeout=60)


if __name__ == "__main__":
    main()
