#!/usr/bin/env python3
"""Review candidate: manual existing-public GHCR upload of two already accepted images.

Never run this during local preparation. --describe is strictly offline.
Actual execution requires a separately authorized GitHub workflow dispatch.
No image build, visibility transition, signing, release or container execution.
"""
import argparse
import hashlib
import json
import importlib.util
import os
from pathlib import Path
import re
import shutil
import stat
import subprocess
import tempfile

REPOSITORY = 'foigent/unirouter'
WORKFLOW_REF = REPOSITORY + '/.github/workflows/publish-accepted-community-images.yml@refs/heads/main'

def require(ok, message):
    if not ok:
        raise RuntimeError(message)

def check_context(env):
    require(env.get('GITHUB_ACTIONS') == 'true', 'Requires the authorized GitHub Actions context.')
    require(env.get('GITHUB_REPOSITORY') == REPOSITORY, 'Wrong repository.')
    require(env.get('GITHUB_REF') == 'refs/heads/main', 'Only protected main is eligible.')
    require(env.get('GITHUB_EVENT_NAME') == 'workflow_dispatch', 'Only explicit manual dispatch is eligible.')
    require(env.get('GITHUB_WORKFLOW_REF') == WORKFLOW_REF, 'Wrong workflow identity.')
    require(re.fullmatch(r'[0-9a-f]{40}', env.get('GITHUB_SHA', '')) is not None, 'Missing workflow commit.')

def describe():
    return {'mode': 'offline_review_only', 'repository': REPOSITORY,
            'stage': 'publisher', 'requires_exact_predecessor_record': True,
            'permissions': {'contents': 'write', 'packages': 'write'},
            'only_existing_public_upload_and_authenticated_identity_verification': True,
            'no_build_visibility_change_release_signing_install_or_container_run': True,
            'no_credential_access_subprocess_or_filesystem_write_in_describe': True}


def command(args, env, inp=None, output=None):
    r = subprocess.run(args, input=inp, stdout=output if output is not None else subprocess.PIPE,
                       stderr=subprocess.PIPE, env=env, check=False)
    # Never print raw child stderr: a download failure could contain a signed URL.
    require(r.returncode == 0, 'Supported command failed: ' + args[0] + ' ' + args[1])
    return r.stdout if output is None else b''

def decode_gh_response(stdout, returncode, allow404=False):
    parts = re.split(rb'\r?\n\r?\n', stdout, maxsplit=1)
    require(len(parts) == 2, 'GitHub response headers unavailable.')
    status = re.match(rb'HTTP/[0-9.]+\s+([0-9]{3})', parts[0])
    require(status is not None, 'GitHub response status unavailable.')
    code = int(status.group(1))
    if allow404 and code == 404:
        return None
    require(code == 200 and returncode == 0, 'GitHub read failed with HTTP ' + str(code))
    return json.loads(parts[1])

def gh(env, path, allow404=False):
    r = subprocess.run(['gh', 'api', '--hostname', 'github.com', '--include', '--method', 'GET',
                        '--header', 'X-GitHub-Api-Version: 2022-11-28', path],
                       stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=env, check=False)
    return decode_gh_response(r.stdout, r.returncode, allow404)

def package_tag(env, image, tag):
    path = 'orgs/foigent/packages/container/' + image['package_name']
    pkg = gh(env, path, allow404=True)
    require(pkg is not None, 'Both existing public packages are required.')
    require(pkg['visibility'] == 'public', 'Public-stage publisher requires already public packages.')
    require((pkg.get('repository') or {}).get('full_name') == REPOSITORY, 'Existing package is not linked to this repository.')
    # A successful, complete native API listing proves tag absence.
    # A failed pull, failed API read or incomplete page never does.
    page = 1; seen = set(); digest = None
    while True:
        rows = gh(env, path + '/versions?state=active&per_page=100&page=' + str(page))
        require(isinstance(rows, list) and len(rows) <= 100, 'Invalid package version page.')
        for row in rows:
            ident = row['id']
            require(type(ident) is int and ident > 0 and ident not in seen, 'Package version listing changed or is ambiguous.')
            seen.add(ident)
            metadata = row['metadata']
            require(metadata['package_type'] == 'container', 'Wrong package version type.')
            tags = metadata['container']['tags']
            require(isinstance(tags, list) and all(isinstance(t, str) for t in tags)
                    and len(set(tags)) == len(tags), 'Invalid package version tags.')
            if tag in tags:
                require(digest is None and re.fullmatch(r'sha256:[0-9a-f]{64}', row['name']) is not None,
                        'Exact tag has an invalid or ambiguous digest.')
                digest = row['name']
        if len(rows) < 100:
            return (True, digest)
        page += 1

def inspected(env, reference):
    q = json.loads(command(['docker', 'image', 'inspect', reference], env))
    require(isinstance(q, list) and len(q) == 1, 'Ambiguous Docker image identity.')
    return q[0]

def check_image(q, image):
    require(q['Id'] == image['config_id'], 'Image config differs from accepted bytes.')
    require(q['RootFS']['Layers'] == image['diff_ids'], 'Image filesystem layers differ from accepted bytes.')
    require(q['Os'] == 'linux' and q['Architecture'] == 'amd64', 'Wrong image platform.')

def image_pin(q, image):
    check_image(q, image)
    digests = {d for d in q.get('RepoDigests', []) if d.startswith(image['repository'] + '@sha256:')}
    require(len(digests) == 1, 'Registry manifest digest is unavailable or ambiguous.')
    pin = digests.pop()
    require(re.fullmatch(re.escape(image['repository']) + r'@sha256:[0-9a-f]{64}', pin) is not None,
            'Invalid registry digest reference.')
    return pin

STAGE_PROGRAM_SHA256 = 'c12627c3c535ddb6884eeeb0e56653863ff98eb839bf1ca78e51f2578ef27260'


def stage_module():
    path = Path(__file__).with_name('community-stage-record.py')
    require(not path.is_symlink() and path.is_file() and path.stat().st_size <= 128*1024
            and hashlib.sha256(path.read_bytes()).hexdigest() == STAGE_PROGRAM_SHA256, 'Stage program identity changed.')
    spec = importlib.util.spec_from_file_location('publisher_stage_contract', path)
    module = importlib.util.module_from_spec(spec); spec.loader.exec_module(module)
    return module


def verify_parent_native(stages, record, env, release):
    parent = record['predecessor']
    run = gh(env, 'repos/' + REPOSITORY + '/actions/runs/' + str(parent['run_id']))
    require(run['id'] == parent['run_id'] and run['run_attempt'] == 1 and run['status'] == 'completed'
            and run['conclusion'] == 'success' and run['head_sha'] == parent['program_commit']
            and run['event'] == 'workflow_dispatch' and run['head_branch'] == 'main'
            and run['path'] == '.github/workflows/community-byte-candidate.yml'
            and run['repository']['id'] == stages.REPOSITORY_ID
            and run['repository']['full_name'] == REPOSITORY
            and run['head_repository']['id'] == stages.REPOSITORY_ID
            and run['actor']['id'] == stages.ACTOR_ID and run['triggering_actor']['id'] == stages.ACTOR_ID, 'Exact producer run identity differs.')
    require(release.get('published_at') is None and release.get('target_commitish') == parent['program_commit']
            and release.get('author', {}).get('id') == 41898282 and release.get('prerelease') is True
            and release.get('tag_name') == 'ur-byte-candidate-'+str(parent['run_id'])+'-attempt1', 'Exact producer draft ownership differs.')
    rows = release['assets']; names = {'backend-image.tar.gz', 'frontend-image.tar.gz',
        'uniroute-upgrade-linux-amd64', 'candidate-evidence.tar.gz', 'SHA256SUMS', 'private-channel-check.txt'}
    require(type(rows) is list and len(rows) == len(names) and {row['name'] for row in rows} == names
            and len({row['id'] for row in rows}) == len(rows), 'Native producer asset closure differs.')
    for row in rows:
        require(type(row['id']) is int and row['id'] > 0 and row.get('state') == 'uploaded'
                and type(row['size']) is int and row['size'] > 0 and row.get('uploader', {}).get('id') == 41898282
                and re.fullmatch(r'sha256:[a-f0-9]{64}', row.get('digest', '')) is not None,
                'Native producer asset identity differs.')
    return {row['id']: row for row in rows}


def verify_parent_evidence(stages, record, env, assets, directory):
    asset = next(row for row in assets.values() if row['name'] == 'candidate-evidence.tar.gz')
    require(asset['size'] <= stages.MAX_EVIDENCE, 'Producer evidence download bound differs.')
    path = directory/'candidate-evidence.tar.gz'
    with path.open('xb') as out:
        path.chmod(0o600)
        command(['gh', 'api', '--hostname', 'github.com', 'repos/' + REPOSITORY + '/releases/assets/' + str(asset['id']),
                 '--header', 'X-GitHub-Api-Version: 2022-11-28', '--header', 'Accept: application/octet-stream'], env, output=out)
    require(path.stat().st_size == asset['size'] and hashlib.sha256(path.read_bytes()).hexdigest() == asset['digest'][7:],
            'Producer evidence native asset bytes differ.')
    result = stages.verify_producer_chain(stages.evidence_files(path), record)
    verifier = assets.get(result['verifier']['asset_id'])
    require(verifier is not None and verifier['name'] == 'uniroute-upgrade-linux-amd64'
            and verifier['size'] == result['verifier']['bytes']
            and verifier['digest'] == 'sha256:' + result['verifier']['sha256'], 'Producer verifier asset differs.')


def execute():
    check_context(os.environ)  # Must precede token access, tools, temp files or API calls.
    stages = stage_module()
    raw_record, record, _ = stages.read_dispatch('publisher')
    draft = record['inputs']['draft_id']; tag = record['inputs']['tag']
    images = [record['inputs']['images'][role] for role in ('backend', 'frontend')]
    require(bool(os.environ.get('GH_TOKEN')), 'Missing temporary repository GITHUB_TOKEN.')
    for binary in ['gh', 'docker']:
        require(shutil.which(binary) is not None, 'Required preinstalled tool unavailable: ' + binary)
    env = os.environ.copy()
    repo = gh(env, 'repos/' + REPOSITORY)
    require(repo['id'] == stages.REPOSITORY_ID and repo['full_name'] == REPOSITORY and repo['visibility'] == 'public', 'Repository identity/cost premise changed.')
    ref = gh(env, 'repos/' + REPOSITORY + '/git/ref/heads/main')
    require(ref['object']['sha'] == env['GITHUB_SHA'], 'Main moved; review the current workflow before writing.')
    release = gh(env, 'repos/' + REPOSITORY + '/releases/' + str(draft))
    require(release['id'] == draft and release['draft'] is True, 'Exact candidate draft identity changed.')
    assets = verify_parent_native(stages, record, env, release)
    states = {}
    # Both targets are checked before any registry write. Never alter visibility,
    # associate an unrelated package, or silently overwrite an existing tag.
    for image in images:
        states[image['role']] = package_tag(env, image, tag)
        asset = assets.get(image['asset_id'])
        require(asset is not None and asset['name'] == image['archive_name'] and asset['size'] == image['archive_bytes'], 'Exact native asset metadata differs.')
        require(asset.get('digest') == 'sha256:' + image['archive_sha256'], 'Native asset server digest differs.')
    parent = Path(env.get('RUNNER_TEMP', ''))
    require(parent.is_absolute() and parent.is_dir() and not parent.is_symlink(), 'Invalid runner temporary directory.')
    directory = Path(tempfile.mkdtemp(prefix='ur-public-image-publish-', dir=parent))
    directory.chmod(0o700)
    verify_parent_evidence(stages, record, env, assets, directory)
    config = directory / 'docker-config'; config.mkdir(mode=0o700)
    # Explicit empty per-registry entry prevents use of a shared credential
    # helper; login writes only this protected disposable-runner configuration.
    config_file = config / 'config.json'
    with config_file.open('x', encoding='utf-8') as f:
        json.dump({'auths': {'ghcr.io': {}}}, f)
    config_file.chmod(0o600)
    env['DOCKER_CONFIG'] = str(config)
    actor = env.get('GITHUB_ACTOR', '')
    require(bool(actor) and len(actor) <= 128, 'Missing publishing actor.')
    command(['docker', 'login', 'ghcr.io', '--username', actor, '--password-stdin'],
            env, inp=(env['GH_TOKEN'] + '\n').encode())
    prepared = []
    for image in images:
        reference = image['repository'] + ':' + tag
        digest = states[image['role']][1]
        if digest is not None:
            # A partial previous attempt may be resumed only by reusing the
            # exact existing tag/content. Missing or differing tags stop here.
            command(['docker', 'pull', reference], env)
            require(image_pin(inspected(env, reference), image) == image['repository'] + '@' + digest,
                    'Existing tag digest changed during pull.')
            operation = 'reused_existing_exact_public_image_no_push'
        else:
            path = directory / image['archive_name']
            with path.open('xb') as f:
                path.chmod(0o600)
                command(['gh', 'api', '--hostname', 'github.com', 'repos/' + REPOSITORY + '/releases/assets/' + str(image['asset_id']),
                         '--header', 'X-GitHub-Api-Version: 2022-11-28',
                         '--header', 'Accept: application/octet-stream'], env, output=f)
            require(path.stat().st_size == image['archive_bytes'], 'Downloaded archive length differs.')
            h = hashlib.sha256()
            with path.open('rb') as f:
                for b in iter(lambda: f.read(1048576), b''): h.update(b)
            require(h.hexdigest() == image['archive_sha256'], 'Downloaded archive SHA256 differs.')
            command(['docker', 'load', '--input', str(path)], env)
            check_image(inspected(env, image['config_id']), image)
            command(['docker', 'image', 'tag', image['config_id'], reference], env)
            operation = 'new_public_push_of_exact_accepted_image'
        prepared.append((image, reference, operation))
    # Validate both actual image contents and fresh target states before
    # either registry write; a conflict in the second image stops both.
    for image in images:
        require(package_tag(env, image, tag) == states[image['role']], 'Package or exact tag changed before upload.')
    results = []
    for image, reference, operation in prepared:
        if states[image['role']][1] is None:
            require(package_tag(env, image, tag) == states[image['role']], 'Package or exact tag changed before upload.')
            command(['docker', 'push', reference], env)
            command(['docker', 'pull', reference], env)
        pin = image_pin(inspected(env, reference), image)
        # Each push must preserve public visibility and the authorized repository association.
        pkg = gh(env, 'orgs/foigent/packages/container/' + image['package_name'])
        require(pkg['visibility'] == 'public' and (pkg.get('repository') or {}).get('full_name') == REPOSITORY,
                'Post-upload package identity/visibility differs; do not change policy automatically.')
        results.append({'role': image['role'], 'repository': image['repository'], 'tag': tag,
                        'registry_digest_reference': pin, 'config_id': image['config_id'],
                        'diff_ids': image['diff_ids'], 'native_archive_sha256': image['archive_sha256'],
                        'asset_id': image['asset_id'], 'operation': operation})
    result = {'status': 'public_images_authenticated_identity_verified_only', 'images': results,
              'product_source': record['product']['commit'], 'candidate_run': record['predecessor']['run_id'], 'draft_id': draft,
              'publisher_workflow_commit': env['GITHUB_SHA'],
              'existing_public_package_metadata_verified': True, 'anonymous_pull_acceptance': False,
              'install_descriptor_or_signed_package_created': False}
    result['stage_result'] = stages.publisher_result(raw_record, env['GITHUB_SHA'], int(env['GITHUB_RUN_ID']), results)
    summary = env.get('GITHUB_STEP_SUMMARY')
    require(bool(summary), 'Missing job summary channel.')
    with open(summary, 'a', encoding='utf-8') as f:
        f.write('### Exact public image receipt\n\n```json\n' + json.dumps(result, indent=2) + '\n```\n')
    print(json.dumps(result, indent=2))
    # Temporary token/config are confined to this disposable runner. No cleanup
    # of user data, shared Docker configuration or existing images is performed.

def main():
    p = argparse.ArgumentParser()
    group = p.add_mutually_exclusive_group(required=True)
    group.add_argument('--describe', action='store_true')
    group.add_argument('--execute-approved-public-stage', action='store_true')
    args = p.parse_args()
    if args.describe:
        print(json.dumps(describe(), indent=2)); return
    execute()

if __name__ == '__main__':
    main()