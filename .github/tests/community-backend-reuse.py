#!/usr/bin/env python3
"""Exact failed-run backend import; no rebuild, retry, publishing or application start."""
import gzip
import hashlib
import io
import json
import os
from pathlib import Path, PurePosixPath
import re
import stat
import tarfile

ORIGIN = json.loads('{"repo_id":1281228065,"actor_id":252312344,"bot_id":41898282,"run_id":39306569744,"workflow_id":387216758,"producer":"edf52280b791c04cc094990bf54b2737d45ba7fb","admission_sha256":"24d07a7eecd874e73b0980dca12aa94623d1dd08ef39909e7ea62f4edf45c43c","draft_id":417633716,"tag":"ur-byte-candidate-39306569744-attempt1","product":{"commit":"9e0240c68a20deebf9bcd0e2cc7b58034d09b696","repository":"foigent/unirouter","schema":"000127_xinsight_communication_policy","tracked_files":2982,"tracked_regular_bytes":50632643,"tree":"8bd942636ca17e48de95d0e531108d303066d923","version":"1.13.0"},"assets":{"backend-image.tar.gz":{"id":657143899,"bytes":21246192,"sha256":"8bfd27a5f9449679cf828e4f9ced287c0d3d9d36985b68463d618587a71a2552","browser_url_sha256":"4cef29ec6f5938529f684c8267daa4a1347387371d255a0a1bb0371aefe58a38"},"candidate-evidence.tar.gz":{"id":657145893,"bytes":438698,"sha256":"89476995ae218662bb3381c87988e586ed5836e159ec324019912b4289fac623","browser_url_sha256":"f4a9e9e2a1d21dec0b9c8bd68e45e2b39e6d5059ef183367e164d3a642ab26f6"},"private-channel-check.txt":{"id":657141347,"bytes":70,"sha256":"89d1af9e785663df92341f16f60e945a0410d6a40b26c20ce57306b44cdf3d34","browser_url_sha256":"4f02fe668a64e5c215cd98a639130815de14d12aadd8591e885b9d9019739b23"}},"metadata_members":17,"source_archive_sha256":"8a68fcb6e18fc83b40692dd2afde30295ad8d21f93493275e666580ca6c03791","saved_backend":{"compressed":{"bytes":21246192,"sha256":"8bfd27a5f9449679cf828e4f9ced287c0d3d9d36985b68463d618587a71a2552"},"image_id":"sha256:777ef815e25c2a0b3e7faa8f6c2efa59c4d74e6d671b5de3f081e4bc9e71c1a0","native_docker_save":{"bytes":57380864,"sha256":"f1d2d55094b38bd8c163ff2fb8283dab8dfdb9f98d3a87dfdc0355dc316ac18b"}},"config_id":"sha256:777ef815e25c2a0b3e7faa8f6c2efa59c4d74e6d671b5de3f081e4bc9e71c1a0","diff_ids":["sha256:e63b02c2b5c761df2cb95e657d744f27d95da4e051235a13c80b88cd68eab188","sha256:fb94ad58758479786d4adfeffe4dce090906e02eebd033e139adedee0d741dda","sha256:dce330452f6caa35c4118aa08a0279278ad948ba3e46c1aef5ebd933f55f9421","sha256:2be94e8af9e12bb6afd69c5805a0f6aa2a9bb90cbefac896571d0ad38a0d3643","sha256:6cbc141a905a0618fb6e525e5099b3bf8d941429cc4a7f9bc6f289646375206d","sha256:2ff3b63321dfeaf9547b00d7a3a2b2ef5413493d56a6b2fca9f1222a728f5f88"],"backend_recipe_sha256":"41411a4c34175ab38c8b1cebac25ef3e14698e0b754ed36ab60cb52d3984dbb7","notice_sha256":"f0f166f98c90e164574d151999b71f0bac8f7ac9fe9ab89c93bd01cbdf269764","public_ring_sha256":"96c91635460ddfb2a14aa1ea97d8445951730e9eb04a33cbdf4320141e61d63d","origin_status":"stopped_partial_candidate","accepted_roles":["backend"]}')
API = 'https://api.github.com'
PREFIX = '/repos/foigent/unirouter'
MIB = 1024**2


class Stop(Exception):
    pass


def need(ok, code):
    if not ok:
        raise Stop(code)


def sha(body):
    return hashlib.sha256(body).hexdigest()


def regular(path, cap):
    def identity(st):
        return (st.st_dev, st.st_ino, st.st_mode, st.st_nlink, st.st_size, st.st_mtime_ns, st.st_ctime_ns)
    st = path.lstat()
    need(stat.S_ISREG(st.st_mode) and st.st_nlink == 1 and st.st_size <= cap, 'origin_regular_file')
    with path.open('rb') as stream:
        need(identity(os.fstat(stream.fileno())) == identity(st), 'origin_file_changed')
        body = stream.read(cap+1)
    need(len(body) == st.st_size and identity(path.lstat()) == identity(st), 'origin_file_changed')
    return body


def encoded(value):
    return (json.dumps(value, sort_keys=True, indent=2, allow_nan=False)+'\n').encode()


def write_new(path, body):
    fd = os.open(path, os.O_CREAT|os.O_EXCL|os.O_WRONLY, 0o600)
    with os.fdopen(fd, 'wb') as stream:
        stream.write(body)


def get(api, path):
    status, value = api.request('GET', path)
    need(status == 200, 'origin_api_read')
    return value


def fresh(api):
    """Validate all three actual assets and anonymous denial before a new draft."""
    need(ORIGIN['origin_status'] == 'stopped_partial_candidate'
         and ORIGIN['accepted_roles'] == ['backend'], 'origin_acceptance_scope')
    # Public run metadata needs no actions permission; draft reads retain the
    # existing contents-write token. No permission expansion or auth fallback.
    status, run = api.request('GET', PREFIX+'/actions/runs/'+str(ORIGIN['run_id']), anonymous=True)
    need(status == 200, 'origin_api_read')
    need(run['id'] == ORIGIN['run_id'] and run['workflow_id'] == ORIGIN['workflow_id']
         and run['run_attempt'] == 1 and run['status'] == 'completed'
         and run['conclusion'] == 'failure' and run['event'] == 'workflow_dispatch'
         and run['head_sha'] == ORIGIN['producer'] and run['head_branch'] == 'main'
         and run['path'] == '.github/workflows/community-byte-candidate.yml'
         and run['repository']['id'] == ORIGIN['repo_id']
         and run['repository']['full_name'] == 'foigent/unirouter'
         and run['head_repository']['id'] == ORIGIN['repo_id']
         and run['actor']['id'] == ORIGIN['actor_id']
         and run['triggering_actor']['id'] == ORIGIN['actor_id'], 'origin_failed_run_identity')
    release = get(api, PREFIX+'/releases/'+str(ORIGIN['draft_id']))
    need(release['id'] == ORIGIN['draft_id'] and release['draft'] is True
         and release['prerelease'] is True and release['published_at'] is None
         and release['target_commitish'] == ORIGIN['producer']
         and release['tag_name'] == ORIGIN['tag']
         and release['author']['id'] == ORIGIN['bot_id'], 'origin_draft_identity')
    rows = release['assets']
    need(len(rows) == 3 and {a['id'] for a in rows} == {a['id'] for a in ORIGIN['assets'].values()},
         'origin_asset_set')
    assets = {a['name']: a for a in rows}
    need(set(assets) == set(ORIGIN['assets']), 'origin_asset_set')
    for name, expected in ORIGIN['assets'].items():
        row = assets[name]
        need(row['id'] == expected['id'] and row['name'] == name and row['state'] == 'uploaded'
             and row['size'] == expected['bytes'] and row['digest'] == 'sha256:'+expected['sha256']
             and row['uploader']['id'] == ORIGIN['bot_id']
             and row['url'] == API+PREFIX+'/releases/assets/'+str(expected['id'])
             and re.fullmatch(r'https://github\.com/foigent/unirouter/releases/download/[A-Za-z0-9._-]+/'+re.escape(name),
                              row['browser_download_url']) is not None
             and sha(row['browser_download_url'].encode()) == expected['browser_url_sha256'],
             'origin_asset_identity')
    status, control = api.request('GET', PREFIX, anonymous=True)
    need(status == 200 and control['id'] == ORIGIN['repo_id'] and control['private'] is False,
         'origin_anonymous_control')
    status, _ = api.request('GET', PREFIX+'/releases/'+str(ORIGIN['draft_id']), anonymous=True)
    need(status == 404, 'origin_draft_audience')
    for row in assets.values():
        for url in (row['url'], row['browser_download_url']):
            need(api.download(url, authenticated=False, require_absent=True) is None, 'origin_asset_audience')
    return assets


def metadata_files(body):
    files = {}; total = 0
    with tarfile.open(fileobj=io.BytesIO(body), mode='r:gz') as archive:
        for row in archive:
            name = row.name
            need(name and not PurePosixPath(name).is_absolute() and '\\' not in name
                 and all(x not in ('', '.', '..') for x in name.split('/'))
                 and row.isfile() and name not in files and len(files) < 8192,
                 'origin_metadata_member')
            total += row.size
            need(0 <= row.size <= 28*MIB and total <= 32*MIB, 'origin_metadata_limit')
            files[name] = archive.extractfile(row).read(row.size+1)
            need(len(files[name]) == row.size, 'origin_metadata_truncated')
    need(len(files) == ORIGIN['metadata_members'], 'origin_metadata_set')
    manifest = json.loads(files['closed-file-manifest.json'])
    need(set(files) == set(manifest['files'])|{'closed-file-manifest.json'}, 'origin_metadata_closure')
    for name, fact in manifest['files'].items():
        need(len(files[name]) == fact['bytes'] and sha(files[name]) == fact['sha256'], 'origin_metadata_digest')
    need(sum(len(b) for n,b in files.items() if n != 'closed-file-manifest.json') == manifest['bytes'],
         'origin_metadata_bytes')
    return files


def validate_metadata(files, fixed, source_export, recipe):
    partial = json.loads(files['partial-terminal.json'])
    need(partial['status'] == ORIGIN['origin_status'] == 'stopped_partial_candidate' and partial['cleanup_complete'] is True
         and partial['error'] == 'Stop'
         and partial['diagnostic']['last_owned_command'] == {'index': 30, 'phase': 'one retained frontend build', 'exit_code': 1},
         'origin_partial_state')
    source = json.loads(files['source-and-fixed-inputs.json'])
    need(source['producer_commit'] == ORIGIN['producer']
         and source['admission_sha256'] == ORIGIN['admission_sha256']
         and source['fixed_inputs'] == fixed and fixed['source'] == ORIGIN['product'], 'origin_source_binding')
    for key in ('commit', 'tree', 'files', 'regular_bytes'):
        need(source['source_export'][key] == source_export[key], 'origin_source_export')
    need(source['source_export']['archive_sha256'] == ORIGIN['source_archive_sha256'], 'origin_source_archive')
    need(sha(recipe) == sha(files['backend.Dockerfile']) == ORIGIN['backend_recipe_sha256'], 'origin_backend_recipe')
    for name, digest in (('component-notices.tar.gz', ORIGIN['notice_sha256']),
                         ('public-keys.json', ORIGIN['public_ring_sha256'])):
        need(sha(files[name]) == digest, 'origin_trust_notice')
    saved = json.loads(files['backend-native-save.json'])
    need(saved == ORIGIN['saved_backend'], 'origin_saved_backend')
    report = json.loads(files['backend-rootfs.json'])
    need(report['image_id'] == ORIGIN['config_id'], 'origin_inventory_image')
    return report


def prepare(api, guard, assets, data, source_export, recipe):
    rows = fresh(api)
    downloaded = {}
    for name in ('private-channel-check.txt', 'candidate-evidence.tar.gz', 'backend-image.tar.gz'):
        expected = ORIGIN['assets'][name]
        target = assets/name if name == 'backend-image.tar.gz' else guard.work/('backend-origin-'+name)
        observed = api.download(rows[name]['url'], target, maximum=expected['bytes'])
        need(observed == {k: expected[k] for k in ('bytes', 'sha256')}, 'origin_download_digest')
        body = regular(target, expected['bytes'])
        need(len(body) == expected['bytes'] and sha(body) == expected['sha256'], 'origin_download_bytes')
        downloaded[name] = target
        if name == 'candidate-evidence.tar.gz':
            files = metadata_files(body)
            report = validate_metadata(files, guard.FIXED, source_export, recipe)
    native = hashlib.sha256(); count = 0
    with gzip.open(downloaded['backend-image.tar.gz'], 'rb') as stream:
        for chunk in iter(lambda: stream.read(65536), b''):
            guard.time_guard(); count += len(chunk)
            need(count <= ORIGIN['saved_backend']['native_docker_save']['bytes'], 'origin_native_size')
            native.update(chunk)
    need({'bytes': count, 'sha256': native.hexdigest()} == ORIGIN['saved_backend']['native_docker_save'],
         'origin_native_digest')
    guard.docker('accepted backend archive load', 'load', '--input', str(downloaded['backend-image.tar.gz']))
    guard.inspect_image('backend', ORIGIN['config_id'])
    image = guard.receipt['images']['backend']
    need(image['id'] == ORIGIN['config_id'] and image['platform'] == {'os':'linux','arch':'amd64'}
         and image['rootfs_diff_ids'] == ORIGIN['diff_ids'], 'origin_loaded_image')
    write_new(data/'backend-origin-evidence.tar.gz', regular(downloaded['candidate-evidence.tar.gz'], 2*MIB))
    return {'files': files, 'report': report, 'saved_backend': ORIGIN['saved_backend'],
            'record': ORIGIN, 'archive': downloaded['backend-image.tar.gz']}


def compare_inventory(actual, origin):
    def entries(rows):
        need(len({x['path'] for x in rows}) == len(rows), 'origin_inventory_duplicate')
        return {x['path']:x for x in rows}
    for name in ('image_id','regular_bytes','apk','dpkg_unparsed'):
        need(actual[name] == origin[name], 'origin_loaded_inventory')
    need(entries(actual['entries']) == entries(origin['entries']), 'origin_loaded_inventory')
    need(entries(actual['notice_filename_candidates']) == entries(origin['notice_filename_candidates']),
         'origin_loaded_notices')


BUILD_PHASES = {'one retained frontend build', 'one offline Linux verifier build'}
MARKERS = {'observer_original_identity': b'original source input identity mismatch',
           'frontend_toolchain': b'Frontend toolchain differs:',
           'vite_build_failure': b'error during build:',
           'bundle_budget': b'bundle budget',
           'npm_error': b'npm error', 'npm_legacy_error': b'npm ERR!',
           'typescript_diagnostic': b'error TS', 'dockerfile_syntax': b'dockerfile parse error'}


def capture_build_diagnostic(guard, data):
    """Only finite marker names escape controlled logs; never raw lines/paths/URLs."""
    result = {'schema': 'controlled-build-diagnostic.v1', 'capture': 'not_controlled_failure',
              'phase': None, 'command_index': None, 'exit_code': None, 'markers': [],
              'raw_log_uploaded': False, 'original_failure_preserved': True, 'persistence': 'pending'}
    try:
        rows = guard.receipt['commands']; row = rows[-1]
        if row['phase'] not in BUILD_PHASES or row['status'] == 'success':
            return result
        result.update(phase=row['phase'], command_index=len(rows),
                      exit_code=row.get('exit_code') if type(row.get('exit_code')) is int else None)
        allowed_env = {'PATH','LANG','LC_ALL','TZ','DOCKER_CONFIG','DOCKER_BUILDKIT','TMPDIR',
                       'GIT_CONFIG_NOSYSTEM','GIT_CONFIG_GLOBAL','GIT_ATTR_NOSYSTEM'}
        if set(guard.child_env)-allowed_env:
            result['capture'] = 'unsafe_child_environment'; return result
        if row.get('log_receiver_complete') is not True:
            result['capture'] = 'receiver_incomplete'; return result
        if row.get('log_limit_exceeded'):
            result['capture'] = 'log_limit_exceeded'; return result
        if row.get('log_receiver_error_class') is not None:
            result['capture'] = 'receiver_error'; return result
        body = regular(guard.work/(str(len(rows))+'.command.log'), 2*MIB)
        if len(body) != row.get('log_bytes') or sha(body) != row.get('log_sha256'):
            result['capture'] = 'log_identity_mismatch'; return result
        result['markers'] = sorted(name for name, marker in MARKERS.items() if marker in body)
        result['capture'] = 'finite_markers_only'
    except Exception:
        result['capture'] = 'capture_unavailable'
    finally:
        try:
            result['persistence'] = 'written'
            write_new(data/'controlled-build-diagnostic.json', encoded(result))
        except Exception:
            result['persistence'] = 'unavailable'  # Never replace/retry the primary failure.
    return result
