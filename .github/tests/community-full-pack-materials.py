"""Assemble the ordinary 26-member release from accepted source, draft, and registry proofs."""
from pathlib import Path
import hashlib
import importlib.util
import json
import os
import re
import subprocess

SOURCE = '564a6d189341d5ad05cccd8b3bdbf5bc4ce66a49'
COMPANIONS = ('postgres', 'redis', 'prometheus', 'grafana')


def read(path):
    return json.loads(path.read_bytes())


def sha(body):
    return hashlib.sha256(body).hexdigest()


def wire(value):
    return (json.dumps(value, ensure_ascii=False, sort_keys=True, indent=2) + '\n').encode()


def identity(body):
    return {'bytes': len(body), 'sha256': sha(body)}


def load_tool(name):
    path = Path(__file__).with_name(f'community-{name}.py')
    spec = importlib.util.spec_from_file_location(name, path)
    result = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(result)
    return result


def put(path, body):
    path.parent.mkdir(parents=True, mode=0o700, exist_ok=True)
    with path.open('xb') as stream:
        os.fchmod(stream.fileno(), 0o600)
        stream.write(body)


def payload_sources(materials):
    for name in materials.GROUPS['runtime']:
        tail = name.removeprefix('payload/')
        if tail == 'deploy/config/prometheus.yml':
            actual = 'infra/prometheus/prometheus.yml'
        elif tail.startswith('deploy/config/'):
            actual = 'infra/' + tail.removeprefix('deploy/config/')
        elif tail.startswith('deploy/'):
            actual = 'deploy/customer/' + tail.removeprefix('deploy/')
        else:
            actual = tail
        yield name, actual
    yield 'payload/licenses/LICENSE', 'LICENSE'
    yield 'payload/licenses/NOTICE', 'NOTICE'
    yield 'payload/licenses/component-notices.tar.gz', '.github/release-inputs/component-notices.tar.gz'


def git_body(source, relative):
    return subprocess.check_output(['git', '-C', str(source), 'show', f'{SOURCE}:{relative}'])


def bound_doc(source, relative):
    parent = (source / relative).parent

    def link(match):
        target = match[1]
        if target.startswith(('#', 'https://', 'http://', 'mailto:')):
            return match[0]
        path, marker, fragment = target.partition('#')
        dest = (parent / path).resolve()
        assert dest.is_relative_to(source) and dest.exists()
        return '](https://github.com/foigent/unirouter/blob/' + SOURCE + '/' + dest.relative_to(source).as_posix() + marker + fragment + ')'

    return re.sub(r'\]\(([^)\s]+)\)', link, git_body(source, relative).decode())


def frontend_disposition(previous, producer_files, product, producer_run, config_id):
    prior_raw = (previous / 'payload/licenses/frontend-disposition.json').read_bytes()
    data = json.loads(prior_raw)
    raw = producer_files['frontend-membership/frontend-membership.json']
    member = json.loads(raw)
    assert not member['unexplained_output_files']
    old_watch = {r['id']: r for r in data['CSS_watched_inputs']}
    watch = {r['id']: r for r in member['watch_inputs'] if r['id'] in old_watch}
    assert watch.keys() == old_watch.keys()
    changes = [{'id': name, 'previous_sha256': old_watch[name]['sha256'], 'current_sha256': row['sha256'],
                'owner': 'UniRouter first-party'} for name, row in watch.items() if old_watch[name] != row]
    assert all(old_watch[r['id']]['package'] is None and watch[r['id']]['package'] is None for r in changes)
    graph = {r['id']: r for r in member['graph']}
    emitted = {}
    for chunk in member['emitted']:
        if chunk['type'] == 'chunk':
            for row in chunk['modules']:
                emitted.setdefault(row['id'], []).append({'chunk': chunk['file'],
                    'original_length': row['original_length'], 'rendered_length': row['rendered_length']})
    for row in data['virtual_modules']:
        observations = emitted.get(row['id'], [])
        row.update(actual_run=producer_run, graph_included=graph[row['id']]['included'],
                   emitted_observations=observations,
                   emitted_positive_bytes=sum(max(0, r['rendered_length']) for r in observations))
    outputs = {r['file']: r for r in member['output_files']}
    css = [{'output': outputs[r['file']], 'observer_original_files': r.get('original_files', []),
            'exact_transform_join_claimed': False,
            'attribution': 'Retain package notices and generator/source supplements. The final output hash is bound; original_files supplies partial CSS attribution.'}
           for r in member['emitted'] if r['file'].endswith('.css')]
    prior_packages = {r['location']: r for r in data['package_notice_materials']}
    assert len(prior_packages) == len(member['packages']) == 209
    for package in member['packages']:
        prior = prior_packages[package['location']]
        assert all(prior[k] == package[k] for k in ('name', 'version', 'declared_license'))
        assert {(n['source'], n['bytes'], n['sha256']) for n in package['notices']} <= {
            (n['original_path'], n['bytes'], n['sha256']) for n in prior['notice_materials']}
    data.update(kind='actual_1156_frontend_disposition', product_source=product, actual_run=producer_run,
                frontend_config_id=config_id, actual_membership_sha256=sha(raw),
                CSS=css, CSS_watched_inputs=list(watch.values()), CSS_input_changes=changes,
                CSS_comparison_base='Accepted 1.15.5 disposition; historical limits retain their original scope.',
                positive_rendered_virtual_count=sum(r['emitted_positive_bytes'] > 0 for r in data['virtual_modules']),
                all_current_outputs_bound_to_final_image=len(outputs),
                unexplained_output_files=member['unexplained_output_files'],
                retained_notice_source_provenance=f'Original material run identities retained. Current emitted observations are from {producer_run}.',
                prior_1155_disposition=identity(prior_raw))
    assert data['complete_license_clearance'] is False
    return data


def source_disposition(previous, evidence_path, producer_files, product, proof, frontend, images, publisher_run):
    raw = (previous / 'payload/licenses/source-disposition.json').read_bytes()
    data = json.loads(raw)
    prior_coverage = data['current_material_coverage']
    old_rows = {(r['role'], r['name']): r for r in data['actual_final_app_APK_rows']}
    rows = []
    for role in ('backend', 'frontend'):
        database = producer_files[f'{role}/databases/lib/apk/db/installed']
        pkg_rows = (dict(line.split(':', 1) for line in block.splitlines() if ':' in line)
                    for block in database.decode().split('\n\n'))
        for index, fields in enumerate(r for r in pkg_rows if 'P' in r):
            old = old_rows[(role, fields['P'])]
            row = dict(old, database=identity(database), image_id=images[role]['config_id'], row_index=index)
            mapped = {'name': 'P', 'version': 'V', 'architecture': 'A', 'declared_license': 'L',
                      'origin': 'o', 'upstream_url': 'U', 'package_build_commit': 'c', 'APK_package_checksum': 'C'}
            row.update({key: fields.get(field) for key, field in mapped.items()})
            changed = {k for k in mapped if row[k] != old[k]}
            if changed:
                assert (role, row['name'], row['version'], row['declared_license']) == ('backend', 'tzdata', '2026f-r0', 'Public-Domain')
                assert changed == {'version', 'package_build_commit', 'APK_package_checksum'}
                row['current_upstream_release'] = 'https://www.iana.org/time-zones/releases'
            row['same_prior_exact_package_identity'] = not changed
            row['same_prior_declared_row_and_original_database'] = not changed and row['database'] == old['database']
            rows.append(row)
    assert len(rows) == 89 and sum(not row['same_prior_exact_package_identity'] for row in rows) == 1
    data.update(kind='actual_1156_source_disposition_with_retained_findings', product_source=product,
                actual_run=proof['producer_run_id'], draft_id=proof['draft_id'], actual_final_app_APK_rows=rows,
                current_material_coverage={
                    'prior_exact_source_disposition': identity(raw),
                    'prior_producer_evidence': prior_coverage['new_candidate_evidence'],
                    'current_producer_evidence': identity(evidence_path.read_bytes()),
                    'unchanged_qualified_system_and_GCC_sources': data['stable_system_and_GCC_sources'],
                    'APK_package_changes': proof['apk_changes'],
                    'unchanged_Go_module_rows': proof['unchanged_observed_go_rows'],
                    'unchanged_npm_package_records': proof['unchanged_observed_npm_package_records'],
                    'unchanged_notice_bodies': proof['unchanged_observed_image_and_npm_notice_bodies'],
                },
                current_frontend_disposition=identity(frontend),
                current_public_image_publication={'run_id': publisher_run, 'registry_manifest_references':
                    [images[r]['registry_digest_reference'] for r in ('backend', 'frontend')]})
    assert data['complete_legal_clearance_claimed'] is False and data['current_package_signature_claimed'] is False
    return data



def assemble(source, previous, output, evidence_path, old_manifest, raw_publisher, receipt):
    """Freeze the normal owner-format inputs. This step performs no network or signing."""
    stages = load_tool('stage-record')
    materials = load_tool('release-materials')
    descriptor = load_tool('install-descriptor')
    stage = stages.validate_record(stages.strict_json(raw_publisher), 'publisher')
    files = stages.evidence_files(evidence_path)
    result = stages.verify_producer_chain(files, stage)
    publisher_result = stages.canonical(receipt['stage_result'])
    bindings = stages.publication_bindings(raw_publisher, publisher_result)
    product = stage['product']
    assert product['commit'] == SOURCE and product['version'] == '1.15.6'
    publisher_run = receipt['stage_result']['run_id']
    publisher = receipt
    images = {r['role']: r for r in receipt['images']}
    assert receipt['images'] == receipt['stage_result']['images']
    producer = stages.strict_json(files['stage/producer-record.json'])
    producer_files = files
    bindings_data = json.loads(files['actual-component-bindings.json'])
    change, = [r for r in bindings_data['actual_final_APK']['backend'] if r['name'] == 'tzdata']
    assert change['version'] == '2026f-r0' and change['license'] == 'Public-Domain'
    proof = {
        'producer_run_id': result['run_id'], 'draft_id': stage['inputs']['draft_id'],
        'actual_linux_verifier': result['verifier'],
        'apk_changes': [{'role': 'backend', 'previous': dict(change, version='2026e-r0'), 'current': change}],
        'unchanged_observed_go_rows': len(bindings_data['actual_go_module_rows']),
        'unchanged_observed_npm_package_records': len(bindings_data['actual_frontend_packages']),
        'unchanged_observed_image_and_npm_notice_bodies': sum(
            '/image-notices/' in name or '/npm-notices/' in name for name in files),
    }
    assert (old_manifest['version'], old_manifest['release_sequence'], old_manifest['target_schema']) == (
        '1.15.5', 22, product['schema'])
    old_artifacts = {r['path']: r for r in old_manifest['artifacts']}
    old_descriptor_raw = (previous / 'payload/install.json').read_bytes()
    assert identity(old_descriptor_raw)['sha256'] == old_artifacts['payload/install.json']['sha256']
    old_descriptor = json.loads(old_descriptor_raw)
    observations = []
    for role, row in images.items():
        observations.append({'role': role, 'Os': 'linux', 'Architecture': 'amd64',
                             'Id': row['config_id'], 'RepoDigests': [row['registry_digest_reference']]})
    for role in COMPANIONS:
        bound = producer['inputs']['images'][role]
        ref = old_descriptor['images'][role]
        assert bound['pull_reference'].endswith('@'+ref.split('@')[1])
        observations.append({'role': role, 'Os': bound['platform']['os'], 'Architecture': bound['platform']['architecture'],
                             'Id': bound['config_id'], 'RepoDigests': [ref]})
    install = descriptor.select_descriptor(old_descriptor, descriptor.publisher_apps(publisher), {'images': observations})
    payload = {target: git_body(source, relative) for target, relative in payload_sources(materials)}
    payload['payload/install.json'] = descriptor.wire(install)
    previous_notices = (previous / 'payload/licenses/THIRD-PARTY-NOTICES.txt').read_bytes()
    assert sha(previous_notices) == old_artifacts['payload/licenses/THIRD-PARTY-NOTICES.txt']['sha256']
    producer_run = proof['producer_run_id']
    header = (
        f'UniRouter 1.15.6 material binding\n\nSource {SOURCE}, producer {producer_run}, publisher {publisher_run}.\n'
        'The backend includes tzdata 2026f-r0 (Public-Domain). See the current source disposition for its observed identity.\n'
        'Other observed package identities and notice bodies match the retained 1.15.5 materials.\n'
        'The retained text below describes prior releases using their original evidence identities.\n\n')
    payload['payload/licenses/THIRD-PARTY-NOTICES.txt'] = header.encode() + previous_notices
    front = frontend_disposition(previous, producer_files, product, producer_run, images['frontend']['config_id'])
    payload['payload/licenses/frontend-disposition.json'] = wire(front)
    payload['payload/licenses/source-disposition.json'] = wire(source_disposition(
        previous, evidence_path, producer_files, product, proof, payload['payload/licenses/frontend-disposition.json'], images, publisher_run))
    verifier = proof['actual_linux_verifier']
    old_prov = read(previous / 'payload/provenance/build-provenance.json')
    ring = identity(producer_files['public-keys.json'])
    assert ring['sha256'] == '96c91635460ddfb2a14aa1ea97d8445951730e9eb04a33cbdf4320141e61d63d'
    prior_producer_evidence = old_prov['community_stage_chain']
    for name in ('install-linux.md', 'install-linux.zh-CN.md'):
        old = (previous / 'payload/docs' / name).read_bytes()
        assert sha(old) == old_artifacts['payload/docs/' + name]['sha256']
        body = old.decode().replace(old_manifest['commit'], SOURCE).replace('1.15.5', '1.15.6')
        if name == 'install-linux.md':
            pattern = r'(verifier is )[0-9]+( bytes, SHA256\n)[0-9a-f]{64}'
        else:
            pattern = r'(校验器为)[0-9]+(字节，SHA256：\n)[0-9a-f]{64}'
        body, matches = re.subn(pattern, lambda m: m[1] + str(verifier['bytes']) + m[2] + verifier['sha256'], body)
        assert matches == 1 and SOURCE in body and ring['sha256'] in body
        payload['payload/docs/' + name] = body.encode()
    heading = ('# UniRouter 1.15.6 recovery and ordinary upgrade\n\n'
               'The supported 1.15.5 / M129 / sequence 22 predecessor can reconstruct the exact 1.15.6 package from the published delta.\n'
               'Keep the verified predecessor and follow the published download and upgrade instructions. Preserve original data and native receipts.\n')
    payload['payload/docs/RECOVERY.md'] = ('\n\n---\n\n'.join(
        [heading, bound_doc(source, 'docs/ops/COMMUNITY-UPGRADE.md'), bound_doc(source, 'deploy/customer/README.md')])).encode()
    source_export = json.loads(producer_files['source-and-fixed-inputs.json'])['source_export']
    upstream = (f'# UniRouter 1.15.6 source and upstream materials\n\n'
                f'Product source: https://github.com/foigent/unirouter/tree/{SOURCE}\n\n'
                f'Exact tracked-source Git archive SHA256: {source_export["archive_sha256"]}\n\n'
                f'Producer: https://github.com/foigent/unirouter/actions/runs/{producer_run}\n'
                f'Publisher: https://github.com/foigent/unirouter/actions/runs/{publisher_run}\n\n'
                'The signed source and frontend dispositions record observed licenses, source routes and attribution limits. '
                'The package carries the retained corresponding GCC source and qualified system materials. '
                'Four dependency images retain their upstream references.\n\n'
                + '\n'.join(f'- {role}: {ref}' for role, ref in sorted(install['images'].items())) + '\n')
    payload['payload/docs/UPSTREAM-SOURCES.md'] = upstream.encode()
    reused = {n for group in ('corresponding_source', 'system_materials') for n in materials.GROUPS[group]}
    rows = [{'path': n, 'size': len(body), 'sha256': sha(body)} for n, body in payload.items()]
    rows.extend({k: old_artifacts[n][k] for k in ('path', 'size', 'sha256')} for n in reused)
    rows.sort(key=lambda r: r['path'])
    assert len(rows) == 25
    provenance = {
        'kind': 'unirouter_actual_1156_release_material_provenance', 'product_source': product,
        'accepted_assembly_run': producer_run, 'accepted_draft_id': proof['draft_id'],
        'actual_public_publisher_run': publisher_run, 'community_stage_chain': bindings,
        'source_export': source_export, 'images': publisher['images'],
        'fixed_input_images': producer['inputs']['images'],
        'actual_linux_verifier': verifier, 'adopted_public_ring': ring,
        'previous_signed_release': {'version': old_manifest['version'], 'sequence': old_manifest['release_sequence'],
            'commit': old_manifest['commit'], 'schema': old_manifest['target_schema'],
            'archive_bytes': 130052679, 'archive_sha256': '46ebdf5d987603aed1989a83f7e7d6a2148214b7f301b80a91413c3964e83e91'},
        'previous_publication_chain': prior_producer_evidence,
        'material_files_before_provenance': rows,
        'retained_dependency_image_references': {role: install['images'][role] for role in COMPANIONS},
        'actual_frontend_membership': {'outputs': front['all_current_outputs_bound_to_final_image'],
            'packages': 209, 'css_outputs': len(front['CSS']), 'virtual_modules': len(front['virtual_modules'])},
    }
    name = 'payload/provenance/build-provenance.json'
    payload[name] = wire(provenance)
    rows = sorted(rows + [{'path': name, 'size': len(payload[name]), 'sha256': sha(payload[name])}], key=lambda r: r['path'])
    assert {r['path'] for r in rows} == set(materials.OWNER)
    current = output / 'current'
    output.mkdir(mode=0o700)
    current.mkdir(mode=0o700)
    for name, body in payload.items():
        put(current / name, body)
    request = {'schema_version': materials.SCHEMA, 'artifacts': rows}
    selected = materials.select_sources(request, current, previous, old_manifest)
    put(output / 'native-material-selection.json', materials.canonical(selected))
    put(output / 'expected-artifacts.json', stages.canonical(rows))
    return selected, provenance
