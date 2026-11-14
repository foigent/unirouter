#!/usr/bin/env python3
"""Rehearse the complete release on the publisher's disposable Linux runner."""
import argparse
from datetime import datetime, timezone
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import platform
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
TARGET_VERSION = '1.15.7'
TARGET_SCHEMA = '000130_upgrade_admission_recovery'
OLD_VERSION = '1.15.6'
OLD_SCHEMA = '000129_upstream_observation_restore_namespace'
PREDECESSOR = '.github/release-inputs/community-upgrade-source.json'
PREDECESSOR_SHA = '82f627fdf4a484e3f3fff860697410dc61ad702d3c2dc9b1e84f86da8b140fa0'
KEY_ID = 'test-only-full-material-rehearsal'
RING_SHA = '96c91635460ddfb2a14aa1ea97d8445951730e9eb04a33cbdf4320141e61d63d'


def fact(path):
    assert path.is_file() and not path.is_symlink()
    with path.open('rb') as body:
        return {'bytes': path.stat().st_size, 'sha256': hashlib.file_digest(body, 'sha256').hexdigest()}


def run(args, env, label, *, should_pass=True, cwd=None):
    process = subprocess.run(args, env=env, cwd=cwd, capture_output=True, check=False, timeout=180)
    if (process.returncode == 0) != should_pass:
        raise RuntimeError(label)
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


def verify(tool, archive, ring, sequence, schema, env, *, should_pass=True):
    raw = run([tool, 'verify', '--package', archive, '--public-keys', ring, '--target-os', 'linux',
               '--target-arch', 'amd64', '--current-sequence', str(sequence), '--current-schema', schema],
               env, 'native_verify_unexpected', should_pass=should_pass)
    return dict(line.split('=', 1) for line in raw.decode().splitlines()) if should_pass else {}


def download_inputs(publisher, stages, record, directory, predecessor):
    env = {k: v for k, v in os.environ.items() if k not in ('GH_DEBUG', 'GIT_CURL_VERBOSE')}
    release = publisher.gh(env, f'repos/{REPO}/releases/{record["inputs"]["draft_id"]}')
    assets = publisher.verify_parent_native(stages, record, env, release)
    publisher.verify_parent_evidence(stages, record, env, assets, directory)
    evidence = directory / 'candidate-evidence.tar.gz'
    parent = stages.verify_producer_chain(stages.evidence_files(evidence), record)
    verifier = directory / 'uniroute-upgrade-linux-amd64'
    expected = parent['verifier']
    with verifier.open('xb') as out:
        publisher.command(['gh', 'api', '--hostname', 'github.com',
            f'repos/{REPO}/releases/assets/{expected["asset_id"]}',
            '-H', 'X-GitHub-Api-Version: 2022-11-28', '-H', 'Accept: application/octet-stream'], env, output=out)
    assert fact(verifier) == {k: expected[k] for k in ('bytes', 'sha256')}
    verifier.chmod(0o700)
    old_name = 'unirouter-' + predecessor['product']['version'] + '-linux-amd64.tar.gz'
    old = directory / old_name
    previous = predecessor['inputs']['config']['release']
    run(['gh', 'release', 'download', previous['tag'], '--repo', REPO, '--pattern', old_name, '--output', old],
        env, 'public_exact_predecessor_download_failed')
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
    assert record['product']['commit'] == expected == record['predecessor']['program_commit']
    assert (record['product']['version'], record['product']['schema']) == (TARGET_VERSION, TARGET_SCHEMA)
    return raw, record


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
    original = verify(verifier, old, prod_ring, 0, OLD_SCHEMA, offline_env)
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
    assert verify(verifier, basis, test_ring, 0, OLD_SCHEMA, offline_env)['release_sequence'] == str(source_sequence)
    assert verify(verifier, target, test_ring, source_sequence, OLD_SCHEMA, offline_env)['release_sequence'] == str(target_sequence)
    verify(verifier, target, prod_ring, source_sequence, OLD_SCHEMA, offline_env, should_pass=False)
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
    raw, record = read_rehearsal_context(stages, publisher, os.environ)
    assert platform.system() == 'Linux' and platform.machine() == 'x86_64'
    source = args.source.resolve()
    assert run(['git', 'rev-parse', 'HEAD'], os.environ, 'product_checkout_failed', cwd=source).decode().strip() == record['product']['commit']
    assert run(['git', 'rev-parse', 'HEAD^{tree}'], os.environ, 'product_tree_failed', cwd=source).decode().strip() == record['product']['tree']
    receipt = stages.strict_json(os.environ['PUBLISHER_RECEIPT'].encode())
    published = stages.verify_publisher_result(raw, receipt['stage_result'])
    assert published['program_commit'] == os.environ['GITHUB_SHA'] and published['run_id'] == int(os.environ['GITHUB_RUN_ID'])
    predecessor_bytes = (source / PREDECESSOR).read_bytes()
    assert stages.sha(predecessor_bytes) == PREDECESSOR_SHA
    predecessor = stages.validate_record(stages.strict_json(predecessor_bytes), 'consumer')
    assert (predecessor['product']['version'], predecessor['product']['schema']) == (OLD_VERSION, OLD_SCHEMA)
    os.umask(0o077)
    with tempfile.TemporaryDirectory(prefix='ur-full-pack-rehearsal-', dir=os.environ['RUNNER_TEMP']) as temporary:
        directory = Path(temporary)
        evidence, verifier, old = download_inputs(publisher, stages, record, directory, predecessor)
        os.environ.pop('GH_TOKEN', None)
        result = rehearse(source, directory, evidence, verifier, old, predecessor, raw, receipt)
    print(json.dumps(result, sort_keys=True))


if __name__ == '__main__':
    try:
        main()
    except (AssertionError, OSError, ValueError, RuntimeError, KeyError, subprocess.SubprocessError) as error:
        print('Complete Linux package rehearsal stopped: ' + type(error).__name__, file=sys.stderr)
        raise SystemExit(1)
