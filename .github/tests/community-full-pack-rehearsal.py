#!/usr/bin/env python3
"""Rehearse the complete release on the publisher's disposable Linux runner."""
import argparse
from contextlib import contextmanager
from datetime import datetime, timezone
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

def module(name):
    # The workflow uses -I. Load the reviewed sibling modules by their own file paths.
    spec = importlib.util.spec_from_file_location(name, Path(__file__).with_name('community-' + name + '.py'))
    value = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(value)
    return value


REPO = 'foigent/unirouter'
TARGET_VERSION = '1.16.0'
TARGET_SCHEMA = '000131_customer_standard_pricing'
OLD_VERSION = '1.15.8'
OLD_SCHEMA = '000131_customer_standard_pricing'
PREDECESSOR = '.github/release-inputs/community-upgrade-source.json'
PREDECESSOR_SHA = 'cbd0b2e0352ce27d7d2957bb2aaa4c3ea703f01aa02124c309063ba6f0cd2b11'
KEY_ID = 'test-only-full-material-rehearsal'
RING_SHA = '96c91635460ddfb2a14aa1ea97d8445951730e9eb04a33cbdf4320141e61d63d'
# The frozen Preview 26 images were published before its package guides changed.
# Only this original record and successful publish job may cross program commits.
RETAINED_PRODUCT_COMMIT = '3e84663f54bd8e57e4637d61bfcdb946a77bc11f'
RETAINED_PRODUCT_TREE = 'e346307c92b165c8396608cc9332eb24268abe44'
RETAINED_RECORD_SHA = '58ade6e3d3141fbef84d9eec98ca86efcc4645e40519d895c54a8d84b2ae8104'
RETAINED_PUBLISHER_RUN = 45463904680
RETAINED_PUBLISHER_JOB = 138693566754
RETAINED_RESULT = Path(__file__).resolve().parents[1] / 'release-inputs/preview26-publisher-result.json'
RETAINED_RESULT_SHA = 'd1a7b295aa098f3be86d6906c95a6ab28c775729c21692924d54dacffd30ecec'
HTTP_FAILURES = {400, 401, 403, 404, 408, 409, 410, 412, 422, 429, 500, 502, 503, 504}



def fact(path):
    assert path.is_file() and not path.is_symlink()
    with path.open('rb') as body:
        return {'bytes': path.stat().st_size, 'sha256': hashlib.file_digest(body, 'sha256').hexdigest()}


class RehearsalFailure(RuntimeError):
    def __init__(self, stage, kind, *, http_status=None, github_request_id=None):
        super().__init__(stage)
        self.report = {'state': 'linux_complete_formal_members_stopped',
                       'stage': stage, 'failure_type': kind,
                       'http_status': http_status, 'github_request_id': github_request_id}


@contextmanager
def phase(label):
    try:
        yield
    except RehearsalFailure:
        raise
    except Exception as error:
        kind = type(error).__name__
        if kind not in {'AssertionError', 'KeyError', 'OSError', 'FileNotFoundError', 'PermissionError',
                        'RuntimeError', 'Stop', 'SubprocessError', 'TimeoutExpired', 'ValueError', 'JSONDecodeError'}:
            kind = 'Exception'
        raise RehearsalFailure(label, kind) from None


def http_failure(response=b'', stderr=b''):
    # Parse a request ID only from an unambiguous, complete gh --include header.
    # Error bodies and redirects may carry credentials and are never reported.
    head, *body = re.split(rb'\r?\n\r?\n', response[:8192], maxsplit=1)
    status_line, *headers = re.split(rb'\r?\n', head)
    status = re.fullmatch(rb'HTTP/[0-9.]+ +([0-9]{3})(?: [^\r\n]*)?', status_line) if body else None
    request_id = None
    if status and all(re.fullmatch(rb"[A-Za-z0-9!#$%&'*+.^_`|~-]+:[^\r\n]*", line) for line in headers):
        ids = [value.strip() for name, value in (line.split(b':', 1) for line in headers)
               if name.lower() == b'x-github-request-id']
        if len(ids) == 1 and re.fullmatch(rb'[0-9A-F]{1,12}(?::[0-9A-F]{1,12}){2,6}', ids[0], re.I):
            request_id = ids[0].decode().upper()
    if status is None:
        status = re.search(rb'^gh: [^\r\n]* \(HTTP ([0-9]{3})\)\s*$', stderr, re.M)
    code = int(status[1]) if status else None
    return {'http_status': code if code in HTTP_FAILURES else None, 'github_request_id': request_id}


def read_github_json(publisher, env, endpoint, label):
    with phase(label):
        reply = subprocess.run(['gh', 'api', '--hostname', 'github.com', '--include', '--method', 'GET',
                                '--header', 'X-GitHub-Api-Version: 2022-11-28', endpoint],
                               env=env, capture_output=True, check=False, timeout=60)
        try:
            return publisher.decode_gh_response(reply.stdout, reply.returncode)
        except (RuntimeError, ValueError):
            raise RehearsalFailure(label, 'GitHubReadError', **http_failure(reply.stdout, reply.stderr)) from None


def download_github_asset(args, env, label, *, output):
    with phase(label):
        reply = subprocess.run(args, env=env, stdout=output, stderr=subprocess.PIPE,
                               check=False, timeout=120)
        if reply.returncode:
            raise RehearsalFailure(label, 'GitHubDownloadError', **http_failure(stderr=reply.stderr))


def run(args, env, label, *, should_pass=True, cwd=None):
    process = subprocess.run(args, env=env, cwd=cwd, capture_output=True, check=False, timeout=180)
    if (process.returncode == 0) != should_pass:
        details = http_failure(stderr=process.stderr) if args[0] == 'gh' else {}
        raise RehearsalFailure(label, 'UnexpectedCommandExit', **details)
    return process.stdout


def unpack_old(path, directory, allowed, predecessor):
    with tarfile.open(path, 'r:gz') as archive:
        first = archive.next()
        assert first is not None and first.name == 'manifest.json' and first.isfile() and first.size < 128 * 1024
        manifest = json.load(archive.extractfile(first))
        old = predecessor['product']
        expected = predecessor['inputs']['config']['release']
        assert (manifest['commit'], manifest['version'], manifest['release_sequence'], manifest['target_schema']) == (
            old['commit'], old['version'], expected['release_sequence'], old['schema'])
        rows = {r['path']: r for r in manifest['artifacts']}
        assert len(rows) == len(allowed) == 26 and set(rows) == allowed
        files = {}
        member = archive.next()
        while member is not None:
            name = member.name
            if name in rows:
                row = rows[name]
                assert member.isfile() and member.size == row['size'] and name not in files
                destination = directory / name
                destination.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
                with archive.extractfile(member) as body, destination.open('xb') as output:
                    shutil.copyfileobj(body, output)
                assert fact(destination) == {'bytes': row['size'], 'sha256': row['sha256']}
                files[name] = destination
            member = archive.next()
        assert set(files) == allowed
    return manifest, files


def pack(tool, manifest, files, private, package, inventory, env):
    assert manifest['expires_at'] == 'never'
    args = [tool, 'pack', '--key-id', KEY_ID, '--private-key', private,
        '--release-sequence', str(manifest['release_sequence']), '--version', manifest['version'],
        '--commit', manifest['commit'], '--created-at', manifest['created_at'], '--no-expiry',
        '--target-os', 'linux', '--target-arch', 'amd64',
        '--min-current-sequence', str(manifest['min_current_sequence']),
        '--max-current-sequence', str(manifest['max_current_sequence']),
        '--min-current-schema', manifest['min_current_schema'], '--target-schema', manifest['target_schema'],
        '--provenance-sha256', manifest['provenance_sha256'], '--expected-artifacts', inventory, '--out', package]
    for name, path in sorted(files.items()):
        args += ['--artifact', f'{name}={path}']
    run(args, env, 'native_pack_failed')


def verify(tool, archive, ring, sequence, schema, env, *, label, should_pass=True):
    raw = run([tool, 'verify', '--package', archive, '--public-keys', ring, '--target-os', 'linux',
               '--target-arch', 'amd64', '--current-sequence', str(sequence), '--current-schema', schema],
               env, label, should_pass=should_pass)
    return dict(line.split('=', 1) for line in raw.decode().splitlines()) if should_pass else {}


def download_inputs(publisher, stages, record, directory, predecessor):
    env = {k: v for k, v in os.environ.items() if k not in ('GH_DEBUG', 'GIT_CURL_VERBOSE')}
    draft = record['inputs']['draft_id']
    release = read_github_json(publisher, env, f'repos/{REPO}/releases/{draft}', 'read_producer_draft')
    with phase('verify_producer_run_and_draft'):
        assets = publisher.verify_parent_native(stages, record, env, release,
            read_json=lambda env, endpoint: read_github_json(publisher, env, endpoint, 'read_producer_run'))
    with phase('verify_producer_evidence'):
        publisher.verify_parent_evidence(stages, record, env, assets, directory,
            download=lambda args, env, output: download_github_asset(args, env, 'download_producer_evidence', output=output))
        evidence = directory / 'candidate-evidence.tar.gz'
        parent = stages.verify_producer_chain(stages.evidence_files(evidence), record)
    verifier = directory / 'uniroute-upgrade-linux-amd64'
    expected = parent['verifier']
    with phase('verify_linux_verifier'):
        with verifier.open('xb') as out:
            download_github_asset(['gh', 'api', '--hostname', 'github.com', '--method', 'GET',
                f'repos/{REPO}/releases/assets/{expected["asset_id"]}',
                '-H', 'X-GitHub-Api-Version: 2022-11-28', '-H', 'Accept: application/octet-stream'],
                env, 'download_linux_verifier', output=out)
        assert fact(verifier) == {k: expected[k] for k in ('bytes', 'sha256')}
        verifier.chmod(0o700)
    with phase('verify_predecessor_download'):
        old_name = 'unirouter-' + predecessor['product']['version'] + '-linux-amd64.tar.gz'
        old = directory / old_name
        previous = predecessor['inputs']['config']['release']
        run(['gh', 'release', 'download', previous['tag'], '--repo', REPO, '--pattern', old_name, '--output', old],
            env, 'download_signed_predecessor')
        assert fact(old) == {'bytes': previous['package_size'], 'sha256': previous['package_sha256']}
    return evidence, verifier, old


def read_rehearsal_context(stages, publisher, env):
    # A job-only retry uses the first publisher job's successful output. This
    # context never admits the publisher itself on a subsequent run attempt.
    publisher.check_context(env)
    inputs = {key: env.get(name, '') for key, name in stages.INPUT_ENV.items()}
    raw, record = stages.from_dispatch(inputs, 'publisher')
    with Path(env['GITHUB_EVENT_PATH']).open('rb') as stream:
        event = stages.strict_json(stream.read(2 * 1024 * 1024 + 1), 2 * 1024 * 1024)
    expected = inputs['expected_workflow_commit']
    assert env['GITHUB_SHA'] == env['GITHUB_WORKFLOW_SHA'] == expected
    assert env['GITHUB_REPOSITORY_ID'] == str(stages.REPOSITORY_ID) and env['GITHUB_ACTOR_ID'] == str(stages.ACTOR_ID)
    assert env['RUNNER_ENVIRONMENT'] == 'github-hosted' and env['RUNNER_OS'] == 'Linux' and env['RUNNER_ARCH'] == 'X64'
    assert int(env['GITHUB_RUN_ATTEMPT']) > 0
    assert event['repository']['id'] == stages.REPOSITORY_ID and event['repository']['full_name'] == REPO
    assert event['sender']['id'] == stages.ACTOR_ID and event['inputs'] == inputs
    publisher.require_subject_binding(raw, record, expected)
    assert (record['product']['version'], record['product']['schema']) == (TARGET_VERSION, TARGET_SCHEMA)
    return raw, record


def replay_context(stages, publisher, env):
    publisher.check_context(env)
    raw, record, _ = stages.read_dispatch('publisher', env)
    assert stages.sha(raw) == RETAINED_RECORD_SHA
    assert record['product'] == {
        'repository': REPO, 'commit': RETAINED_PRODUCT_COMMIT, 'tree': RETAINED_PRODUCT_TREE,
        'version': TARGET_VERSION, 'schema': TARGET_SCHEMA,
    }
    assert env['GITHUB_SHA'] != RETAINED_PRODUCT_COMMIT
    assert record['predecessor']['program_commit'] == RETAINED_PRODUCT_COMMIT
    return raw, record


def retained_receipt(stages, publisher, env, raw):
    with phase('verify_retained_publisher_result'):
        assert not RETAINED_RESULT.is_symlink()
        with RETAINED_RESULT.open('rb') as stream:
            frozen = stream.read(stages.MAX_RAW + 1)
        assert stages.sha(frozen) == RETAINED_RESULT_SHA
        result = stages.verify_publisher_result(raw, stages.strict_json(frozen))
        assert stages.canonical(result) == frozen
        assert (result['run_id'], result['program_commit']) == (RETAINED_PUBLISHER_RUN, RETAINED_PRODUCT_COMMIT)
        assert all(image['operation'] == 'new_public_push_of_exact_accepted_image' for image in result['images'])
    repo = f'repos/{REPO}'
    branch = read_github_json(publisher, env, f'{repo}/branches/main', 'read_protected_main')
    with phase('verify_protected_main'):
        assert branch['protected'] is True and branch['commit']['sha'] == env['GITHUB_SHA']
    run = read_github_json(publisher, env, f'{repo}/actions/runs/{RETAINED_PUBLISHER_RUN}', 'read_original_publisher_run')
    with phase('verify_original_publisher_run'):
        assert (run['id'], run['workflow_id'], run['run_attempt'], run['event'],
                run['head_sha'], run['head_branch'], run['status'], run['conclusion']) == (
                    RETAINED_PUBLISHER_RUN, 387706956, 1, 'workflow_dispatch',
                    RETAINED_PRODUCT_COMMIT, 'main', 'completed', 'success')
        assert run['path'] == '.github/workflows/publish-accepted-community-images.yml'
        assert run['repository']['id'] == run['head_repository']['id'] == stages.REPOSITORY_ID
        assert run['repository']['full_name'] == REPO
        assert run['actor']['id'] == run['triggering_actor']['id'] == stages.ACTOR_ID
    job = read_github_json(publisher, env, f'{repo}/actions/jobs/{RETAINED_PUBLISHER_JOB}', 'read_original_publisher_job')
    with phase('verify_original_publisher_job'):
        assert (job['id'], job['run_id'], job['run_attempt'], job['name'], job['status'], job['conclusion']) == (
            RETAINED_PUBLISHER_JOB, RETAINED_PUBLISHER_RUN, 1, 'publish-public', 'completed', 'success')
        assert job['head_sha'] == RETAINED_PRODUCT_COMMIT and job['labels'] == ['ubuntu-24.04']
    return {'images': result['images'], 'stage_result': result}


def rehearse(source, directory, evidence, verifier, old, predecessor, raw_record, receipt):
    stages = module('stage-record')
    assembler = module('full-pack-materials')
    materials = module('release-materials')
    record = stages.validate_record(stages.strict_json(raw_record), 'publisher')
    prod_ring = directory / 'public-keys.json'
    ring_raw = stages.evidence_files(evidence)['public-keys.json']
    assert hashlib.sha256(ring_raw).hexdigest() == RING_SHA
    assembler.put(prod_ring, ring_raw)
    offline_env = {k: v for k, v in os.environ.items() if k not in ('GH_TOKEN', 'GITHUB_TOKEN', 'GH_DEBUG', 'GIT_CURL_VERBOSE')}
    previous_release = predecessor['inputs']['config']['release']
    source_sequence = previous_release['release_sequence']
    target_sequence = source_sequence + 1
    original = verify(verifier, old, prod_ring, 0, OLD_SCHEMA, offline_env, label='verify_signed_predecessor')
    assert (original['verified'], original['package_sha256'], original['release_sequence'], original['commit']) == (
        'true', previous_release['package_sha256'], str(source_sequence), predecessor['product']['commit'])
    previous = directory / 'exact-predecessor-members'
    old_manifest, old_files = unpack_old(old, previous, set(materials.OWNER), predecessor)
    frozen = directory / 'frozen-target-members'
    selected, provenance = assembler.assemble(source, previous, frozen, evidence, old_manifest, predecessor, raw_record, receipt)
    target_inventory = frozen / 'expected-artifacts.json'
    old_inventory = directory / 'old-expected-artifacts.json'
    assembler.put(old_inventory, stages.canonical(old_manifest['artifacts']))

    keygen = directory / 'uniroute-test-keygen'
    run(['go', 'build', '-buildvcs=false', '-mod=readonly', '-o', keygen, './cmd/uniroute-license'],
        offline_env, 'existing_test_keygen_build_failed', cwd=source)
    private, test_ring = directory / 'test-only-key', directory / 'test-only-ring.json'
    run([keygen, 'keygen', '--key-id', KEY_ID, '--private-out', private, '--public-out', test_ring],
        offline_env, 'temporary_test_key_creation_failed')
    basis, target = directory / 'fixture-basis.tar.gz', directory / 'fixture-target.tar.gz'
    pack(verifier, old_manifest, old_files, private, basis, old_inventory, offline_env)
    target_manifest = dict(old_manifest, commit=record['product']['commit'], version=TARGET_VERSION,
        target_schema=TARGET_SCHEMA, release_sequence=target_sequence,
        min_current_sequence=0, max_current_sequence=source_sequence,
        created_at=datetime.now(timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ'),
        provenance_sha256=fact(frozen / 'current/payload/provenance/build-provenance.json')['sha256'])
    pack(verifier, target_manifest, {r['path']: Path(r['file_path']) for r in selected['sources']},
         private, target, target_inventory, offline_env)
    private.unlink()
    assert verify(verifier, basis, test_ring, 0, OLD_SCHEMA, offline_env, label='verify_test_predecessor')['release_sequence'] == str(source_sequence)
    assert verify(verifier, target, test_ring, source_sequence, OLD_SCHEMA, offline_env, label='verify_test_target')['release_sequence'] == str(target_sequence)
    verify(verifier, target, prod_ring, source_sequence, OLD_SCHEMA, offline_env,
           label='verify_production_trust_refusal', should_pass=False)
    with tarfile.open(target, 'r:gz') as archive:
        member_modes = {member.name: f'{member.mode:04o}' for member in archive if member.name.startswith('payload/')}
    assert member_modes == {row['path']: '0644' for row in selected['artifacts']}
    delta = directory / 'fixture-delta.tar.gz'
    args = ['--basis', basis, '--public-keys', test_ring, '--current-schema', OLD_SCHEMA]
    run([verifier, 'create-delta', *args, '--target', target, '--out', delta], offline_env, 'native_create_delta_failed')
    target_fact, delta_fact = fact(target), fact(delta)
    spec = importlib.util.spec_from_file_location('exact_bootstrap', source / 'deploy/community/bootstrap.py')
    bootstrap = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(bootstrap)
    inputs = {'verifier': verifier, 'public_keys': test_ring, 'package': target, 'delta': delta}

    def entry(role):
        value = fact(inputs[role])
        return {'size': value['bytes'], 'sha256': value['sha256'], 'url': 'https://example.invalid/' + role}

    basis_fact = fact(basis)
    basis_entry = {'size': basis_fact['bytes'], 'sha256': basis_fact['sha256']}
    release = {'version': TARGET_VERSION, 'commit': record['product']['commit'], 'release_sequence': target_sequence,
               'key_id': KEY_ID, 'target_schema': TARGET_SCHEMA}
    raw = stages.canonical({'schema_version': 'unirouter.community-bootstrap.v2',
        'platform': {'os': 'linux', 'arch': 'amd64'}, 'release': release,
        'assets': {role: entry(role) for role in ('verifier', 'public_keys', 'package')},
        'delta': {'basis': basis_entry, 'asset': entry('delta')}})
    requested = []

    def fetch(asset, destination):
        role = asset['url'].rsplit('/', 1)[-1]
        assert role in ('public_keys', 'delta') and role not in requested
        if role == 'delta':
            # The shipped bootstrap owns the existing transfer marker.
            with delta.open('rb') as body, destination.open('r+b') as marker:
                shutil.copyfileobj(body, marker)
        else:
            assembler.put(destination, inputs[role].read_bytes())
        requested.append(role)

    prepared = directory / 'prepared-release'
    bootstrap.prepare(bootstrap.Bootstrap(raw, stages.sha(raw)), prepared, fetch=fetch,
                      source_package=basis, source_verifier=verifier)
    rebuilt = prepared / 'release.tar.gz'
    assert requested == ['public_keys', 'delta'] and fact(rebuilt) == target_fact
    with target.open('rb') as expected, rebuilt.open('rb') as actual:
        for chunk in iter(lambda: expected.read(1024 * 1024), b''):
            assert actual.read(len(chunk)) == chunk
        assert actual.read(1) == b''
    assert delta_fact['bytes'] < target_fact['bytes']
    return {'state': 'linux_complete_formal_members_rehearsed_with_temporary_test_key',
            'product': record['product'], 'original_predecessor': fact(old), 'native_linux_verifier': fact(verifier),
            'predecessor_sequence': source_sequence, 'target_sequence': target_sequence, 'frozen_target_artifacts': json.loads(target_inventory.read_bytes()),
            'target_archive_member_modes': member_modes,
            'frozen_target_inventory': fact(target_inventory), 'fixture_basis': fact(basis), 'fixture_target': target_fact,
            'fixture_delta': delta_fact, 'rebuilt_identical': True, 'full_package_requests': 0, 'formal_trust_rejected_fixture': True,
            'test_private_key_removed': not private.exists(), 'publisher': provenance['community_stage_chain']['publisher_result_sha256']}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source', type=Path, required=True)
    args = parser.parse_args()
    stages, publisher = module('stage-record'), module('image-publisher')
    env = os.environ
    replay = not env.get('PUBLISHER_RECEIPT')
    with phase('verify_dispatch_context'):
        raw, record = (replay_context if replay else read_rehearsal_context)(stages, publisher, env)
        assert platform.system() == 'Linux' and platform.machine() == 'x86_64'
    with phase('verify_product_checkout'):
        source = args.source.resolve()
        assert run(['git', 'rev-parse', 'HEAD'], env, 'read_product_commit', cwd=source).decode().strip() == record['product']['commit']
        assert run(['git', 'rev-parse', 'HEAD^{tree}'], env, 'read_product_tree', cwd=source).decode().strip() == record['product']['tree']
    with phase('verify_workflow_checkout'):
        program = Path(__file__).resolve().parents[2]
        assert run(['git', 'rev-parse', 'HEAD'], env, 'read_workflow_commit', cwd=program).decode().strip() == env['GITHUB_SHA']
    if replay:
        public_env = {key: value for key, value in env.items() if key not in ('GH_DEBUG', 'GIT_CURL_VERBOSE')}
        receipt = retained_receipt(stages, publisher, public_env, raw)
    else:
        with phase('verify_publisher_output'):
            receipt = stages.strict_json(env['PUBLISHER_RECEIPT'].encode())
            published = stages.verify_publisher_result(raw, receipt['stage_result'])
            assert published['program_commit'] == env['GITHUB_SHA'] and published['run_id'] == int(env['GITHUB_RUN_ID'])
    with phase('verify_predecessor_record'):
        predecessor_bytes = (source / PREDECESSOR).read_bytes()
        assert stages.sha(predecessor_bytes) == PREDECESSOR_SHA
        predecessor = stages.validate_record(stages.strict_json(predecessor_bytes), 'consumer')
        assert (predecessor['product']['version'], predecessor['product']['schema']) == (OLD_VERSION, OLD_SCHEMA)
    os.umask(0o077)
    with tempfile.TemporaryDirectory(prefix='ur-full-pack-rehearsal-', dir=os.environ['RUNNER_TEMP']) as temporary:
        directory = Path(temporary)
        evidence, verifier, old = download_inputs(publisher, stages, record, directory, predecessor)
        os.environ.pop('GH_TOKEN', None)
        with phase('build_verify_and_reconstruct_complete_package'):
            result = rehearse(source, directory, evidence, verifier, old, predecessor, raw, receipt)
    result.update(rehearsal_program_commit=env['GITHUB_SHA'], rehearsal_run_id=int(env['GITHUB_RUN_ID']),
                  original_publisher_program=receipt['stage_result']['program_commit'],
                  original_publisher_run_id=receipt['stage_result']['run_id'], replayed_publisher_receipt=replay)
    print(json.dumps(result, sort_keys=True))


if __name__ == '__main__':
    try:
        with phase('rehearsal_setup'):
            main()
    except RehearsalFailure as error:
        print(json.dumps(error.report, sort_keys=True), file=sys.stderr)
        raise SystemExit(1)
