#!/usr/bin/env python3
"""Consume the fixed real public UR release on one disposable hosted runner.

Describe/validate perform no network, Docker, key or output operations. Execution never builds
images, creates signing keys, publishes packages or claims persistent Target.
"""
import argparse
import base64
import contextlib
import datetime as dt
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import platform
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile
import uuid
import urllib.error
import urllib.request

IMMUTABLE_SHA256 = "2f3a78dbea7f9d3d21c29a2dfdee51d3adbca4fef2eb3c3f75f7ad8fc5e4d12e"
SCHEMA = "000127_xinsight_communication_policy"
WORKFLOW = "foigent/unirouter/.github/workflows/community-release-acceptance.yml@refs/heads/main"
RELEASE_KEYS = {"release_sequence", "created_at", "expires_at", "tag", "bootstrap_url",
                "bootstrap_size", "bootstrap_sha256", "package_size", "package_sha256"}


class Stop(Exception):
    """Only static authored labels may be emitted."""


def require(ok, message):
    if not ok:
        raise Stop(message)


FIRST_CUSTOMER_NAME = "Foigent Community Acceptance"
FIRST_CUSTOMER_LIMIT = 16 * 1024
FIRST_CUSTOMER_STATE_SQL = """SELECT jsonb_build_object(
 'admin',(SELECT jsonb_build_object('id',id,'org_id',org_id,'username',username,
   'role',role,'organization_role',organization_role,'status',status,
   'auth_epoch',auth_epoch,'auth_provider',auth_provider) FROM users WHERE username='admin'),
 'users',(SELECT count(*) FROM users),
 'members',(SELECT count(*) FROM project_members),
 'orgs',(SELECT coalesce(jsonb_agg(jsonb_build_object('id',id,'name',name,
   'status',status,'org_type',org_type,'parent_id',parent_id) ORDER BY id),'[]') FROM orgs),
 'wallets',(SELECT coalesce(jsonb_agg(jsonb_build_object('org_id',org_id,'currency',currency,
   'balance_micros',balance_micros,'granted_balance_micros',granted_balance_micros,
   'frozen_micros',frozen_micros,'total_spent_micros',total_spent_micros,
   'auto_freeze',auto_freeze,'low_balance_threshold_cents',low_balance_threshold_cents)
   ORDER BY org_id),'[]') FROM wallets),
 'projects',(SELECT coalesce(jsonb_agg(jsonb_build_object('id',id,'org_id',org_id,
   'slug',slug,'status',status) ORDER BY id),'[]') FROM projects),
 'rotations',(SELECT count(*) FROM audit_logs WHERE action='user.password_reset'
   AND (user_id,org_id)=(SELECT id,org_id FROM users WHERE username='admin')),
 'bootstrap_audits',(SELECT coalesce(jsonb_agg(jsonb_build_object('org_id',org_id,
   'user_id',user_id,'resource',resource,'resource_id',resource_id,
   'client_request_id',client_request_id) ORDER BY id),'[]')
   FROM audit_logs WHERE action='bootstrap.first_customer'),
 'release_state',(SELECT coalesce(jsonb_agg(jsonb_build_array(release_sequence,version,commit,
   package_sha256,trusted_key_id,target_schema,md5(manifest::text)) ORDER BY release_sequence),'[]') FROM release_state),
 'release_history',(SELECT coalesce(jsonb_agg(jsonb_build_array(release_sequence,version,commit,
   package_sha256,trusted_key_id,target_schema,md5(manifest::text)) ORDER BY release_sequence),'[]') FROM release_history)
)::text"""



# This is deliberately public synthetic test data from the repository's native
# CLI integration test, never an existing installation/provider credential.
FIRST_USE_TEST_INPUT = "replacement-password-456"
CLIENT_REQUEST_ID_HEADER = "X-UniRouter-Client-Request-ID"


def verified_app_image_id(reference, expected, image):
    role = expected.get("role")
    require(role in ("backend", "frontend") and
            re.fullmatch(r"ghcr\.io/foigent/unirouter-" + role + r"@sha256:[0-9a-f]{64}", reference) is not None,
            "App repository or manifest reference differs.")
    manifest_id = reference.split("@", 1)[1]
    require(re.fullmatch(r"sha256:[0-9a-f]{64}", expected.get("config_id", "")) is not None and
            isinstance(expected.get("diff_ids"), list) and expected["diff_ids"] and
            all(re.fullmatch(r"sha256:[0-9a-f]{64}", v) is not None for v in expected["diff_ids"]),
            "Pinned app config or ordered layers are invalid.")
    require(image.get("Os") == "linux" and image.get("Architecture") == "amd64" and
            reference in (image.get("RepoDigests") or []) and
            image.get("Id") in (expected["config_id"], manifest_id) and
            (image.get("RootFS") or {}).get("Layers") == expected["diff_ids"],
            "Anonymous app identity or ordered layers differ from the one pinned image.")
    return image["Id"]


def password_choice_delta(before, after):
    keys = {"admin", "users", "members", "orgs", "wallets", "projects", "rotations",
            "bootstrap_audits", "release_state", "release_history"}
    require(set(before) == set(after) == keys and type(before["rotations"]) is int and before["rotations"] == 1 and after["rotations"] == 2 and
            type(before["admin"].get("auth_epoch")) is int, "Unexpected password-choice baseline or reset count.")
    wanted = dict(before, rotations=2, admin=dict(before["admin"], auth_epoch=before["admin"]["auth_epoch"] + 1))
    require(immutable_digest(after) == immutable_digest(wanted),
            "Password choice changed protected identities, release state or seed inventory.")


def fixture_admin_login(password, *, expected_status=200):
    require(isinstance(password, str) and password and len(password.encode()) <= 72 and
            "\r" not in password and "\n" not in password and
            type(expected_status) is int and expected_status in (200, 401),
            "Invalid synthetic administrator login input.")
    request = urllib.request.Request("http://127.0.0.1:13000/api/v1/auth/login",
        data=json.dumps({"username": "admin", "password": password}).encode(),
        headers={"Content-Type": "application/json"}, method="POST")
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoFirstCustomerRedirect())
    try:
        with opener.open(request, timeout=20) as response:
            require(expected_status == response.status == 200, "Unexpected fixture login status.")
            raw = response.read(FIRST_CUSTOMER_LIMIT + 1)
            require(len(raw) <= FIRST_CUSTOMER_LIMIT, "Fixture login response exceeded its bound.")
            value = strict_json(raw)
            require(value.get("organization_role") == "platform_admin" and
                    isinstance(value.get("token"), str) and value["token"], "Chosen fixture input did not authenticate.")
            return value
    except urllib.error.HTTPError as error:
        code = error.code
        error.close()
        require(expected_status == code == 401, "Fixture administrator login failed.")
        return None
    except Stop:
        raise
    except Exception:
        raise Stop("Fixture administrator login failed.") from None


class NoFirstCustomerRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def first_customer_request(token, request_id, *, dry_run, expected_status):
    """One normal synthetic fixture POST; no credentials or response bodies in errors."""
    require(isinstance(token, str) and token and '\r' not in token and '\n' not in token
            and isinstance(request_id, str) and re.fullmatch(r'[a-z0-9]{32}', request_id) is not None
            and type(dry_run) is bool and expected_status in (200, 201),
            "Invalid first-customer fixture request.")
    body = json.dumps({'organization_name': FIRST_CUSTOMER_NAME, 'dry_run': dry_run}).encode()
    request = urllib.request.Request('http://127.0.0.1:13000/api/v1/bootstrap/first-customer',
        data=body, headers={'Content-Type': 'application/json', 'Authorization': 'Bearer ' + token,
                            CLIENT_REQUEST_ID_HEADER: request_id}, method='POST')
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoFirstCustomerRedirect())
    try:
        with opener.open(request, timeout=20) as response:
            require(response.status == expected_status, "Unexpected first-customer HTTP status.")
            raw = response.read(FIRST_CUSTOMER_LIMIT + 1)
            require(len(raw) <= FIRST_CUSTOMER_LIMIT, "First-customer response exceeded its bound.")
            return strict_json(raw)
    except Stop:
        raise
    except Exception:
        raise Stop("First-customer fixture HTTP request failed.") from None


def first_customer_preview(value, before, *, expected_rotations=1):
    require(type(expected_rotations) is int and expected_rotations in (1, 2), "Invalid explicit rotation baseline.")
    admin = before['admin']
    require(before['users'] == 1 and before['members'] == 0 and before['rotations'] == expected_rotations
            and not before['bootstrap_audits'] and len(before['orgs']) == 1
            and before['orgs'][0]['id'] == admin['org_id']
            and len(before['projects']) == len(before['wallets']) == 1
            and before['projects'][0]['org_id'] == before['wallets'][0]['org_id'] == admin['org_id']
            and before['projects'][0]['slug'] == 'default', "Unexpected first-customer seed inventory.")
    require(isinstance(value, dict) and value.get('contract_version') == 'unirouter.first-customer-bootstrap-preview.v1'
            and value.get('dry_run') is True and value.get('ready') is True
            and value.get('checkpoint') == 'fresh_seed' and value.get('organization_name') == FIRST_CUSTOMER_NAME
            and type(value.get('platform_org_id')) is int and value['platform_org_id'] == admin['org_id']
            and isinstance(value.get('would_remove'), dict)
            and all(type(v) is int for v in value['would_remove'].values())
            and value.get('would_remove') == {'project_id': before['projects'][0]['id'],
                                            'zero_wallet_org_id': admin['org_id']},
            "First-customer preview differs from the exact seed inventory.")


def first_customer_result(value, *, idempotent, customer_id=None):
    require(isinstance(value, dict) and set(value) == {'organization_id','organization_name','wallet_balance_micros','idempotent'}
            and type(value.get('organization_id')) is int and value['organization_id'] > 0
            and value.get('organization_name') == FIRST_CUSTOMER_NAME
            and type(value.get('wallet_balance_micros')) is int and value['wallet_balance_micros'] == 0
            and value.get('idempotent') is idempotent
            and (customer_id is None or value['organization_id'] == customer_id),
            "First-customer create or replay result differs.")
    return value['organization_id']


def first_customer_transition(before, after, customer_id, request_id, *, restarted=False):
    require(customer_id != before['admin']['org_id'], "Customer reused the platform organization.")
    for key in ('admin','users','members','rotations','release_state','release_history'):
        require(immutable_digest(after[key]) == immutable_digest(before[key]),
                "First-customer changed protected administrator or release metadata.")
    platform = before['orgs'][0]
    customer = {'id':customer_id,'name':FIRST_CUSTOMER_NAME,'status':'active','org_type':'customer','parent_id':None}
    require(immutable_digest(after['orgs']) == immutable_digest(sorted([platform, customer], key=lambda r:r['id'])),
            "First-customer organization inventory differs.")
    wallet = dict(before['wallets'][0], org_id=customer_id)
    require(wallet['currency'] == 'CNY' and wallet['auto_freeze'] is False
            and all(type(wallet[k]) is int and wallet[k] == 0 for k in
                    ('balance_micros','granted_balance_micros','frozen_micros','total_spent_micros','low_balance_threshold_cents'))
            and immutable_digest(after['wallets']) == immutable_digest([wallet]), "First-customer wallet is not exactly zero CNY.")
    require(immutable_digest(after['bootstrap_audits']) == immutable_digest([{'org_id':before['admin']['org_id'],
            'user_id':before['admin']['id'],'resource':'organization','resource_id':str(customer_id),
            'client_request_id':request_id}]), "First-customer bootstrap audit identity differs.")
    projects = after['projects']
    if restarted:
        require(len(projects) == 1 and type(projects[0]['id']) is int and projects[0]['id'] > 0
                and projects[0]['id'] != before['projects'][0]['id']
                and type(projects[0]['org_id']) is int and projects[0]['org_id'] == customer_id and projects[0]['slug'] == 'default'
                and projects[0]['status'] == 'active', "Restart did not select the canonical customer project.")
    else:
        require(projects == [], "Platform seed project remained after first-customer bootstrap.")


def strict_json(raw):
    def pairs(items):
        result = {}
        for key, value in items:
            require(key not in result, "Duplicate JSON metadata field.")
            result[key] = value
        return result
    return json.loads(raw, object_pairs_hook=pairs)


def sha(path):
    value = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            value.update(block)
    return value.hexdigest()


def immutable_digest(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(",", ":")).encode()).hexdigest()


def load_config(repo):
    path = repo / ".github/release-inputs/community-formal-release.json"
    require(not path.is_symlink() and path.stat().st_size <= 64 * 1024, "Unexpected fixed consumer input.")
    return strict_json(path.read_bytes())


def asset_url(release, name):
    return "https://github.com/foigent/unirouter/releases/download/" + release["tag"] + "/" + name


STAGE_PROGRAM_SHA256 = 'c12627c3c535ddb6884eeeb0e56653863ff98eb839bf1ca78e51f2578ef27260'


def stage_module():
    path = Path(__file__).with_name('community-stage-record.py')
    require(path.is_file() and not path.is_symlink() and path.stat().st_size <= 128*1024
            and sha(path) == STAGE_PROGRAM_SHA256, "Stage program identity changed.")
    spec=importlib.util.spec_from_file_location('consumer_stage_contract',path)
    value=importlib.util.module_from_spec(spec);spec.loader.exec_module(value);return value


def target_schema(config):
    return config['immutable']['product']['schema']


def validate_current_fixed(config, stage_record):
    stages=stage_module(); stages.validate_record(stage_record,'consumer')
    require(config == stage_record['inputs']['config'], 'Consumer input differs from its exact stage record.')
    fixed=config['immutable']
    require(type(fixed['min_current_sequence']) is int and type(fixed['max_current_sequence']) is int
            and 0 <= fixed['min_current_sequence'] <= fixed['max_current_sequence'] < 2**63
            and type(fixed['historical_sequence_reserved_through']) is int
            and fixed['max_current_sequence'] <= fixed['historical_sequence_reserved_through'] < 2**63,
            'Unsupported predecessor sequence window.')
    require(fixed['min_current_schema'] in stages.HEADS
            and fixed['min_current_schema'] <= target_schema(config), 'Unsupported predecessor schema window.')
    for item,maximum in ((fixed['verifier'],32*1024*1024),(fixed['public_ring'],1024)):
        require(type(item) is dict and set(item)=={'size','sha256'} and type(item['size']) is int
                and 0 < item['size'] <= maximum and re.fullmatch('[a-f0-9]{64}',item['sha256']) is not None,
                'Consumer bounded file identity differs.')
    require(re.fullmatch('[a-f0-9]{64}',fixed['native_acceptance_helper_sha256']) is not None
            and re.fullmatch('[a-f0-9]{64}',fixed['provenance_sha256']) is not None, 'Consumer program/provenance identity differs.')
    bootstrap=fixed['bootstrap_script']
    require(set(bootstrap)=={'url','size','sha256'} and type(bootstrap['size']) is int and 0 < bootstrap['size'] <= 128*1024
            and re.fullmatch('[a-f0-9]{64}',bootstrap['sha256']) is not None
            and bootstrap['url']=='https://raw.githubusercontent.com/foigent/unirouter/'+fixed['product']['commit']+'/deploy/community/bootstrap.py',
            'Consumer bootstrap source differs.')
    require(type(fixed['images']) is dict and set(fixed['images'])=={'backend','frontend','postgres','redis','prometheus','grafana'},
            'Consumer six-image closed set differs.')
    repositories={'backend':'ghcr.io/foigent/unirouter-backend','frontend':'ghcr.io/foigent/unirouter-frontend',
                  'postgres':'(?:docker.io/library/)?postgres','redis':'(?:docker.io/library/)?redis',
                  'prometheus':'(?:docker.io/)?prom/prometheus','grafana':'(?:docker.io/)?grafana/grafana'}
    for role,reference in fixed['images'].items():
        require(type(reference) is str and re.fullmatch(repositories[role]+'@sha256:[a-f0-9]{64}',reference) is not None,
                'Consumer image digest reference differs.')
    artifacts=fixed['artifacts']; require(type(artifacts) is list and 1 <= len(artifacts) <= 64, 'Consumer payload bound differs.')
    names=[]
    for row in artifacts:
        require(type(row) is dict and {'path','size','sha256'} <= set(row) <= {'path','size','sha256','media_type'}, 'Consumer payload fields differ.')
        name=row['path'];require(type(name) is str and name.startswith('payload/') and len(name)<=256
            and all(part not in ('','.','..') for part in name.split('/')) and re.fullmatch('[A-Za-z0-9._/+-]+',name) is not None
            and type(row['size']) is int and 0 <= row['size'] <= 512*1024*1024
            and re.fullmatch('[a-f0-9]{64}',row['sha256']) is not None, 'Consumer payload identity differs.')
        if 'media_type' in row:require(type(row['media_type']) is str and len(row['media_type'])<=128,'Consumer media type differs.')
        names.append(name)
    require(names==sorted(set(names)) and sum(row['size'] for row in artifacts)<=512*1024*1024, 'Consumer payload closure differs.')
    required={'payload/deploy/install.py','payload/install.json','payload/provenance/build-provenance.json'}
    if target_schema(config)=='000128_upstream_release_observation':
        required.update({'payload/deploy/upgrade.py','payload/deploy/recovery.py','payload/scripts/source-budget-restore-rebase.sql'})
    require(required <= set(names), 'Consumer required installation/upgrade payload is missing.')
    provenance=next((row for row in artifacts if row['path']=='payload/provenance/build-provenance.json'),None)
    require(provenance is not None and provenance['sha256']==fixed['provenance_sha256'] and provenance['size']<=128*1024,
            'Consumer signed provenance artifact differs.')
    require(type(fixed['app_image_identities']) is list and len(fixed['app_image_identities'])==2
            and {row['role'] for row in fixed['app_image_identities']}=={'backend','frontend'}, 'Consumer app image closure differs.')
    for row in fixed['app_image_identities']:
        require(set(row)=={'role','repository','tag','registry_digest_reference','config_id','diff_ids','native_archive_sha256','asset_id','operation'}
                and row['repository']=='ghcr.io/foigent/unirouter-'+row['role']
                and row['registry_digest_reference']==fixed['images'][row['role']]
                and re.fullmatch('sha256:[a-f0-9]{64}',row['config_id']) is not None
                and type(row['diff_ids']) is list and 0<len(row['diff_ids'])<=32
                and all(re.fullmatch('sha256:[a-f0-9]{64}',layer) is not None for layer in row['diff_ids'])
                and re.fullmatch('[a-f0-9]{64}',row['native_archive_sha256']) is not None
                and type(row['asset_id']) is int and row['asset_id']>0, 'Consumer native app image identity differs.')
    for key in ('accepted_assembly_run','accepted_draft_id','actual_private_publisher_run'):
        require(type(fixed[key]) is int and fixed[key]>0,'Consumer native predecessor identity differs.')
    require(fixed['actual_private_publisher_run']==stage_record['predecessor']['run_id'], 'Consumer publisher run differs.')


def validate(config, *, ready=False, stage_record=None):
    require(set(config) == {"schema_version", "state", "immutable", "release"} and
            config["schema_version"] == "unirouter.formal-consumer-input.v1" and
            config["state"] in ("incomplete", "ready"), "Invalid consumer config shape.")
    fixed, release = config["immutable"], config["release"]
    if stage_record is None:
        require(immutable_digest(fixed) == IMMUTABLE_SHA256, "Fixed product/image/payload/trust identity changed.")
    else:
        validate_current_fixed(config, stage_record)
    require(set(release) == RELEASE_KEYS, "Unexpected release decision fields.")
    require(fixed["manifest_schema_version"] == "unirouter.upgrade-manifest.v2" and
            fixed["expires_at"] == release["expires_at"] == "never", "Explicit signed non-expiring policy required.")
    def missing_bindings(v, prefix):
        if v is None: return [prefix]
        if isinstance(v, dict): return [p for k, x in v.items() for p in missing_bindings(x, prefix + "." + k)]
        if isinstance(v, list): return [p for i, x in enumerate(v) for p in missing_bindings(x, prefix + "." + str(i))]
        return []
    missing = [k for k, v in release.items() if v is None] + missing_bindings(fixed, "binding")
    if ready:
        require(config["state"] == "ready" and not missing,
                "Formal release input is incomplete; no output, network or Docker operation was attempted.")
        n = release["release_sequence"]
        require(type(n) is int and fixed["historical_sequence_reserved_through"] < n < 2 ** 63,
                "Invalid admitted release sequence.")
        require(release["tag"] == "v" + fixed["product"]["version"] + "-preview." + str(n), "Release tag differs from the admitted preview identity.")
        parse = lambda v: dt.datetime.fromisoformat(v.replace("Z", "+00:00"))
        created = parse(release["created_at"])
        require(created.tzinfo is not None and created.utcoffset() == dt.timedelta(0) and
                created.strftime("%Y-%m-%dT%H:%M:%SZ") == release["created_at"] and
                created <= dt.datetime.now(dt.timezone.utc) + dt.timedelta(minutes=5),
                "Release creation time is not current canonical UTC.")
        require(release["bootstrap_url"] == asset_url(release, "community-bootstrap.json"),
                "Bootstrap URL is outside the exact public preview asset.")
        for k, maximum in (("bootstrap_size", 32 * 1024), ("package_size", 512 * 1024 ** 2)):
            require(type(release[k]) is int and 0 < release[k] <= maximum, "Invalid bounded actual download size.")
        for k in ("bootstrap_sha256", "package_sha256"):
            require(isinstance(release[k], str) and re.fullmatch(r"[a-f0-9]{64}", release[k]),
                    "Actual pinned download SHA256 is missing.")
    return missing


def bootstrap_binding(config, value):
    f, r = config["immutable"], config["release"]
    require(value["release"] == {"version": f["product"]["version"], "commit": f["product"]["commit"],
                "release_sequence": r["release_sequence"], "key_id": f["key_id"], "target_schema": target_schema(config)},
            "Public bootstrap release binding differs.")
    expected = {"verifier": ("uniroute-upgrade-linux-amd64", f["verifier"]),
                "public_keys": ("public-keys.json", f["public_ring"]),
                "package": ("unirouter-" + f["product"]["version"] + "-linux-amd64.tar.gz",
                            {"size": r["package_size"], "sha256": r["package_sha256"]})}
    require(set(value["assets"]) == set(expected), "Bootstrap asset closed set differs.")
    for role, (name, identity) in expected.items():
        require(value["assets"][role] == dict(identity, url=asset_url(r, name)),
                "Public download/trust identity differs.")


def manifest_binding(config, manifest):
    f, r = config["immutable"], config["release"]
    expected = {"schema_version": "unirouter.upgrade-manifest.v2", "product": "unirouter",
                "key_id": f["key_id"], "signature_algorithm": "ed25519",
                "release_sequence": r["release_sequence"], "version": f["product"]["version"],
                "commit": f["product"]["commit"], "created_at": r["created_at"], "expires_at": r["expires_at"],
                "target_os": "linux", "target_arch": "amd64", "min_current_sequence": f["min_current_sequence"],
                "max_current_sequence": f["max_current_sequence"], "min_current_schema": f["min_current_schema"], "target_schema": target_schema(config),
                "provenance_sha256": f["provenance_sha256"], "artifacts": f["artifacts"]}
    require(manifest == expected, "Verified real manifest differs from the exact reviewed payload contract.")


def native_guard(repo):
    require(os.environ.get("GITHUB_ACTIONS") == "true" and
            os.environ.get("RUNNER_ENVIRONMENT") == "github-hosted" and
            os.environ.get("GITHUB_REPOSITORY") == "foigent/unirouter" and
            os.environ.get("GITHUB_EVENT_NAME") == "workflow_dispatch" and
            os.environ.get("GITHUB_REF") == "refs/heads/main" and
            os.environ.get("GITHUB_WORKFLOW_REF") == WORKFLOW and
            platform.system() == "Linux" and platform.machine() == "x86_64" and os.geteuid() == 0,
            "Requires the exact disposable hosted Linux workflow, not a workstation or Target.")
    commit = os.environ.get("GITHUB_SHA", "")
    require(re.fullmatch(r"[a-f0-9]{40}", commit) and
            Path(os.environ.get("GITHUB_WORKSPACE", "")).resolve() == repo.resolve(), "Wrong checkout identity.")
    env = {k: os.environ[k] for k in ("PATH", "HOME", "LANG", "LC_ALL") if k in os.environ}
    result = subprocess.run(["git", "-c", "safe.directory=" + str(repo), "rev-parse", "HEAD"],
                            cwd=repo, env=env, capture_output=True)
    require(result.returncode == 0 and result.stdout.decode().strip() == commit, "Workflow checkout differs from actual GitHub commit.")
    return {"workflow_commit": commit, "run": os.environ.get("GITHUB_RUN_ID"),
            "attempt": os.environ.get("GITHUB_RUN_ATTEMPT")}


def load_module(path, expected_sha, name):
    require(not path.is_symlink() and sha(path) == expected_sha, "Existing helper source changed.")
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def reference_sources(repo, config):
    fixed = config["immutable"]
    for path, expected in ((repo / ".github/tests/community-installer.py", fixed["native_acceptance_helper_sha256"]),
                           (repo / "deploy/community/bootstrap.py", fixed["bootstrap_script"]["sha256"])):
        require(path.is_file() and not path.is_symlink() and sha(path) == expected,
                "Fixed existing consumer/helper source changed before preparation.")


@contextlib.contextmanager
def clean_environment(env):
    old = dict(os.environ)
    try:
        os.environ.clear()
        os.environ.update(env)
        yield
    finally:
        os.environ.clear()
        os.environ.update(old)


def make_acceptance(repo, root, config, context, *, stage_record=None):
    fixed = config["immutable"]
    base = load_module(repo / ".github/tests/community-installer.py",
                       fixed["native_acceptance_helper_sha256"], "accepted_native_installer")
    bootstrap = load_module(repo / "deploy/community/bootstrap.py", fixed["bootstrap_script"]["sha256"],
                            "accepted_product_bootstrap")

    class Formal(base.Acceptance):
        def __init__(self):
            self.repo, self.root, self.config = repo, root, config
            self.logs = root / "logs"
            self.logs.mkdir(mode=0o700)
            self.instances = root / "instances"
            self.instances.mkdir(mode=0o700)
            home = root / "anonymous-home"
            home.mkdir(mode=0o700)
            docker_config = home / ".docker"
            docker_config.mkdir(mode=0o700)
            base.private_write(docker_config / "config.json", b'{"auths":{"ghcr.io":{}}}\n')
            self.env = {k: os.environ[k] for k in ("PATH", "LANG", "LC_ALL") if k in os.environ}
            self.env.update(HOME=str(home), DOCKER_CONFIG=str(docker_config))
            self.docker = shutil.which("docker")
            require(self.docker is not None, "Docker is unavailable.")
            self.docker = str(Path(self.docker).resolve())
            self.release = root / "prepared-release"  # Remains absent until the shipped bootstrap creates it.
            self.verifier, self.ring = self.release / "uniroute-upgrade", self.release / "public-keys.json"
            self.bootstrap_file, self.bootstrap_script = root / "bootstrap.json", root / "bootstrap.py"
            self.bootstrap_sha = config["release"]["bootstrap_sha256"]
            self.commit, self.tree, self.version = [fixed["product"][k] for k in ("commit", "tree", "version")]
            self.schema = target_schema(config)
            self.release_sequence = config["release"]["release_sequence"]
            self.suffix = uuid.uuid4().hex[:10]
            self.images, self.image_ids = fixed["images"], {}
            self.phase = "native prerequisites"
            self.result = {"schema": "unirouter.formal-community-consumer.v1", "status": "running", "checks": [],
                           "platform": "linux/amd64", "source": self.commit, "tree": self.tree, "version": self.version,
                           "release_sequence": self.release_sequence, "provider_calls": 0, "builds": 0,
                           "signatures_generated": 0, "persistent_Target_acceptance": False, **context}

        def bootstrap_command(self, action, directory, project):
            return [sys.executable, "-I", "-B", self.bootstrap_script, action, "--bootstrap", self.bootstrap_file,
                    "--bootstrap-sha256", self.bootstrap_sha, "--directory", self.release,
                    "--install-directory", directory, "--name", project, "--port", "13000"]

        def prepare(self):
            r = config["release"]
            bootstrap.download({"url": r["bootstrap_url"], "size": r["bootstrap_size"],
                                "sha256": r["bootstrap_sha256"]}, self.bootstrap_file)
            anchor = bootstrap.Bootstrap(self.bootstrap_file.read_bytes(), r["bootstrap_sha256"])
            bootstrap_binding(config, anchor.value)
            bootstrap.download(fixed["bootstrap_script"], self.bootstrap_script)
            self.run("prepare real owner-signed public release", self.bootstrap_command("prepare", root / "unused-install", "unused"))
            with tarfile.open(self.release / "release.tar.gz", "r:gz") as archive:
                first = archive.next()
                require(first is not None and first.isfile() and first.name == "manifest.json" and first.size <= 128 * 1024,
                        "Verified signed manifest is unavailable.")
                manifest_binding(config, strict_json(archive.extractfile(first).read()))
            if 'stage_chain' in fixed:
                provenance=self.release/'payload/provenance/build-provenance.json'
                require(provenance.is_file() and not provenance.is_symlink() and provenance.stat().st_size <= 128*1024
                        and sha(provenance)==fixed['provenance_sha256'], 'Signed stage provenance bytes differ.')
                proof=strict_json(provenance.read_bytes())
                stage_module().verify_publication_bindings(proof['community_stage_chain'], config, stage_record)
            descriptor = strict_json((self.release / "payload/install.json").read_bytes())
            require(descriptor == {"schema_version": "unirouter.install.v1", "images": self.images},
                    "Real signed descriptor differs from actual accepted digest references.")
            self.other_ring = root / "wrong-public-ring.json"
            base.private_write(self.other_ring, json.dumps({fixed["key_id"]: base64.b64encode(bytes(32)).decode()}).encode())
            self.result.update(real_owner_signed_public_package_verified=True,
                               package_sha256=r["package_sha256"], bootstrap_sha256=r["bootstrap_sha256"])
            self.passed("actual full signed package and independent trust bindings verified")
            expected = {a["role"]: a for a in fixed["app_image_identities"]}
            for service, ref in self.images.items():
                self.run("anonymous pull " + service, [self.docker, "pull", ref])
                values = json.loads(self.run("inspect exact image " + service, [self.docker, "image", "inspect", ref]))
                require(len(values) == 1, "Ambiguous anonymous image.")
                image = values[0]
                require(image["Os"] == "linux" and image["Architecture"] == "amd64" and
                        ref in image.get("RepoDigests", []), "Anonymous image digest/platform differs.")
                if service in expected:
                    verified_app_image_id(ref, expected[service], image)
                self.image_ids[service] = image["Id"]
            self.passed("six anonymous digest pulls and both exact app configs and ordered layers verified")

        def negative_checks(self, directory, project):
            before = self.inventory(), base.files(self.release), base.files(self.instances)
            self.run("read-only real bootstrap check", self.bootstrap_command("check", directory, project))
            self.run("read-only real installer check", self.command("check", directory, project))
            require(before == (self.inventory(), base.files(self.release), base.files(self.instances)), "Read-only check changed resources.")
            code, _ = self.run("reject wrong public key", self.command("check", directory, project, ring=self.other_ring), ok=False)
            require(code != 0, "Wrong public key was accepted.")
            descriptor = self.release / "payload/install.json"
            original = descriptor.read_bytes()
            try:
                with descriptor.open("ab") as stream:
                    stream.write(b" ")
                tampered = self.inventory(), base.files(self.release), base.files(self.instances)
                code, _ = self.run("reject modified signed descriptor", self.command("check", directory, project), ok=False)
                require(code != 0 and tampered == (self.inventory(), base.files(self.release), base.files(self.instances)),
                        "Tampered descriptor check did not refuse without writes.")
            finally:
                descriptor.write_bytes(original)
            require(before == (self.inventory(), base.files(self.release), base.files(self.instances)), "Negative trust checks changed resources.")
            volume = project + "_postgres_data"
            self.run("create exact empty synthetic collision", [self.docker, "volume", "create", volume])
            collision = self.run("read exact collision", [self.docker, "volume", "inspect", volume])
            code, _ = self.run("reject exact-name collision", self.command("install", directory, project), ok=False)
            require(code != 0 and not directory.exists() and collision ==
                    self.run("verify collision unchanged", [self.docker, "volume", "inspect", volume]), "Conflict refusal failed.")
            self.run("remove this exact empty test collision", [self.docker, "volume", "rm", volume])
            require(before == (self.inventory(), base.files(self.release), base.files(self.instances)), "Conflict check changed resources.")
            self.passed("check purity, wrong key, tampered signed descriptor and exact collision refusal")

        def facts(self, directory, project):
            state = json.loads((directory / "install-state.json").read_text())
            plan = json.loads((directory / "evidence/install.json").read_text())
            require(state["image_ids"] == self.image_ids, "installed images differ from producer inspection")
            actual = self.target(directory, project)
            require(all(plan["target"][k] == v for k, v in actual.items()), "database identity differs from original plan")
            require(self.sql(directory, project, "SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1") == target_schema(config), "schema differs from exact signed target")
            comment = self.sql(directory, project, "SELECT obj_description('public.schema_migrations'::regclass)")
            record = json.loads(comment.split("\n", 1)[1])
            require(record["state"] == "completed" and record["plan"] == plan, "fresh owner is not completed for the original plan")
            rotation = self.sql(directory, project, "SELECT count(*) FROM audit_logs WHERE action='user.password_reset' AND user_id=1 AND org_id=1")
            require(rotation == "1", "administrator password rotation count is not one")
            release = json.loads(self.sql(directory, project, "SELECT jsonb_build_object('sequence',release_sequence,'version',version,'commit',commit,'package',package_sha256,'schema',target_schema)::text FROM release_state"))
            require(release == {"sequence": self.release_sequence, "version": self.version, "commit": self.commit,
                                "package": self.config["release"]["package_sha256"], "schema": target_schema(config)}, "initial release registration mismatch")
            ids = self.run("read installed containers", self.compose(directory, project) + ["ps", "-aq"]).splitlines()
            containers = json.loads(self.run("inspect installed containers", [self.docker, "inspect", *ids]))
            require(len(containers) == 6, "six services were not installed")
            for container in containers:
                name = container["Config"]["Labels"]["com.docker.compose.service"]
                require(container["Image"] == state["image_ids"][name], "running image mismatch")
                require(container["State"]["Running"], "installed service is not running")
                for binds in (container["HostConfig"].get("PortBindings") or {}).values():
                    require(name == "frontend" and all(b["HostIp"] == "127.0.0.1" and b["HostPort"] == "13000" for b in binds), "non-loopback or unexpected published service")
                if name in ("backend", "frontend", "postgres", "redis"):
                    require(container["State"].get("Health", {}).get("Status") == "healthy", "service health failed")
            return {"target": actual, "plan_sha256": sha(directory / "evidence/install.json"),
                    "record": record, "release": release, "rotation": rotation,
                    "files": state["files"], "installation_id": state["installation_id"], "images": state["image_ids"]}

        def first_customer_state(self, directory, project):
            # sql() retains its stdout: this query selects only allowlisted safe
            # identity/counter fields, never password hashes, sessions or audit bodies.
            return strict_json(self.sql(directory, project, FIRST_CUSTOMER_STATE_SQL))

        def chosen_password_checks(self, directory, project):
            self.phase = "native caller-selected fixture password"
            before = self.first_customer_state(directory, project)
            retained = lambda: (sha(directory / "admin-password"), sha(directory / "install-state.json"),
                                sha(directory / "evidence/install.json"), base.files(directory / "runtime"))
            original_files = retained()
            generated = (directory / "admin-password").read_text().strip()
            self.run("choose synthetic password through native private stdin", self.compose(directory, project) +
                ["exec", "-T", "backend", "./uniroute", "admin", "set-password", "--username", "admin",
                 "--org-id", "1", "--password-stdin"], data=(FIRST_USE_TEST_INPUT + "\n").encode())
            after = self.first_customer_state(directory, project)
            password_choice_delta(before, after)
            require(retained() == original_files, "Native password choice changed original installer files.")
            fixture_admin_login(generated, expected_status=401)
            fixture_admin_login(FIRST_USE_TEST_INPUT)
            self.chosen_fixture_input = FIRST_USE_TEST_INPUT
            self.result["caller_selected_password"] = {"initial_installer_rotations": 1, "final_rotations": 2,
                "auth_epoch_advanced_once": True, "generated_old_input_refused": True,
                "original_installer_files_unchanged": True}
            self.passed("native chosen password advances exactly one epoch/audit and preserves original installation")

        def first_customer_checks(self, directory, project):
            self.phase = "first-customer normal authenticated preview"
            password = self.chosen_fixture_input
            def login():
                value = strict_json(self.request('http://127.0.0.1:13000/api/v1/auth/login',
                    data=json.dumps({'username':'admin','password':password}).encode()))
                require(value.get('organization_role') == 'platform_admin' and value.get('token'),
                        "Original fixture administrator cannot log in.")
                return value['token']
            token, intent = login(), uuid.uuid4().hex
            before = self.first_customer_state(directory, project)
            preview = first_customer_request(token, intent, dry_run=True, expected_status=200)
            first_customer_preview(preview, before, expected_rotations=2)
            require(immutable_digest(before) == immutable_digest(self.first_customer_state(directory, project)),
                    "First-customer preview changed seed or protected metadata.")
            self.passed("real first-customer preview is read-only and names exact empty-seed IDs")
            self.phase = "first-customer exact commit and known-success replay"
            created = first_customer_request(token, intent, dry_run=False, expected_status=201)
            customer_id = first_customer_result(created, idempotent=False)
            after = self.first_customer_state(directory, project)
            first_customer_transition(before, after, customer_id, intent)
            replay = first_customer_request(token, intent, dry_run=False, expected_status=200)
            first_customer_result(replay, idempotent=True, customer_id=customer_id)
            require(immutable_digest(after) == immutable_digest(self.first_customer_state(directory, project)),
                    "Known-success first-customer replay created duplicate state.")
            self.passed("real first-customer commit and same-intent replay create one zero-wallet customer and one audit")
            self.run("stop only the synthetic fixture backend", self.compose(directory, project) + ['stop','-t','30','backend'])
            self.run("restart fixture backend with native bounded health wait", self.compose(directory, project) +
                     ['up','-d','--no-deps','--wait','--wait-timeout','120','--pull','never','--no-build','backend'])
            self.request('http://127.0.0.1:13000/health/ready')
            login()  # The same original password must still work after bootstrap/restart.
            restarted = self.first_customer_state(directory, project)
            first_customer_transition(before, restarted, customer_id, intent, restarted=True)
            self.result['first_customer'] = {'organization_id':customer_id,
                'platform_org_id':before['admin']['org_id'], 'default_project_id':restarted['projects'][0]['id'],
                'preview_read_only':True,'same_intent_replay':True,'chosen_admin_password_usable':True,
                'protected_release_and_rotation_preserved':True,'provider_calls':0}
            self.passed("native restart selects the customer default project and preserves original admin and release history")

        def execute(self):
            self.preflight()
            self.prepare()
            self.project = "ur-formal-" + self.suffix
            self.directory = self.instances / "interrupted"
            self.negative_checks(self.directory, self.project)
            self.interrupt_after_plan(self.directory, self.project)
            self.installed_checks(self.directory, self.project)
            self.chosen_password_checks(self.directory, self.project)
            self.first_customer_checks(self.directory, self.project)
            self.result["status"] = "passed"
            self.result.pop("installer_stop", None)

    return Formal()


def execute(config, repo, stage_record=None):
    require(stage_record is not None, 'Execution requires an exact current consumer stage record.')
    validate(config, ready=True, stage_record=stage_record)
    context = native_guard(repo)  # All invalid inputs/context stop before creating any destination.
    reference_sources(repo, config)
    os.umask(0o077)
    root = Path(tempfile.mkdtemp(prefix="ur-formal-community-consumer-", dir="/var/tmp")).resolve()
    root.chmod(0o700)
    fixture = make_acceptance(repo, root, config, context, stage_record=stage_record)
    try:
        with clean_environment(fixture.env):
            fixture.execute()
    except BaseException:
        fixture.result.update(status="failed", phase=fixture.phase, error="consumer_check_failed_redacted")
        with clean_environment(fixture.env):
            fixture.failure_facts()
    # The runner lifecycle owns these exact temporary resources. No broad cleanup,
    # second manager, artifact/cache upload or persistent Target claim.
    report = json.dumps(fixture.result, separators=(",", ":"))
    require(len(report.encode()) <= 32 * 1024, "Allowlisted report exceeded its complete bound.")
    print(report, flush=True)
    return 0 if fixture.result["status"] == "passed" else 1


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("describe", "validate", "execute"))
    parser.add_argument("--stage-dispatch", action="store_true", help="read the exact current manual stage record")
    args = parser.parse_args(argv)
    try:
        repo = Path(__file__).resolve().parents[2]
        stage_record=None
        if args.stage_dispatch:
            _, stage_record, _=stage_module().read_dispatch('consumer')
            config=stage_record['inputs']['config']
        else:
            require(args.action != 'execute', 'Execution requires an exact current consumer stage record.')
            config = load_config(repo)  # Retained read-only audit of the historical signed input.
        missing = validate(config, ready=args.action != "describe", stage_record=stage_record)
        if stage_record is not None: reference_sources(repo, config)
        if args.action == "describe":
            print(json.dumps({"source": config["immutable"]["product"]["commit"], "payload_entries": len(config["immutable"]["artifacts"]),
                              "ready": config["state"] == "ready" and not missing,
                              "missing_decisions": missing, "network_Docker_or_output_operations": 0}))
            return 0
        if args.action == "validate":
            print("Actual public input shape accepted; no network, Docker or destination operation.")
            return 0
        return execute(config, repo, stage_record)
    except BaseException as error:
        print("Formal consumer stopped: " + (str(error) if isinstance(error, Stop) else type(error).__name__), file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
