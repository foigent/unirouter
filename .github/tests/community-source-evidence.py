#!/usr/bin/env python3
"""One separately approved source/notice observation; no release or installation.

The existing facts producer is hash-bound and imported for its guards. Its main,
facts projection, full builds and report schema are never called or modified.
"""
import bz2
import datetime
import gzip
import hashlib
import io
import json
import os
from pathlib import Path, PurePosixPath
import platform
import re
import shlex
import stat
import sys
import tarfile
import time
import types

# Frozen source/image/notice constants are inserted here from the reviewed plan.
# BEGIN FIXED INPUTS
SOURCE = json.loads('{"repository":"foigent/unirouter","commit":"a6b03cc8bc2494b735ea1da4018beb7b4af44d3f","tree":"2f4ae72d1446a27e6919f1509c06bd8fc8ac2a47","version":"1.14.1","schema":"000127_xinsight_communication_policy","tracked_files":2995,"tracked_regular_bytes":50828614}')
IMAGES = json.loads('{"backend-builder":{"input_reference":"docker.io/library/golang:1.25.12-alpine","index_digest":"sha256:56961d79ea8129efddcc0b8643fd8a5416b4e6228cfd477e3fd61deb2672c587","pull_reference":"docker.io/library/golang@sha256:2b6edeb8c6b1071bfa16473f24bb7b7da0b1579009f97bb1542f239b14aabd8f","config_id":"sha256:c7d080ccd6695ac0b69509d2ca5686ddc405b27019ea1452221307deb3c76bd8","platform":{"os":"linux","architecture":"amd64","variant":null},"rootfs_diff_ids":["sha256:34884abbe92863fce933ed7c39c0e045631af0ed86d5cc0dfbdf9fdca426ce3c","sha256:49c5f4bd4150345d6bdfa7c7e677d73fe03cc742598901e5ac40959d9a3072f2","sha256:c11523315a2840edc8c35a91f1f9f475a1da16bed3de0eae0f03a674873b475a","sha256:c91b45a2b070045f87ad6b195cdef95f77cef782df535b0bf4d91d0606548325","sha256:5f70bf18a086007016e948b04aed3b82103a36bea41755b6cddfaf10ace3c6ef"],"source_annotation":"https://github.com/docker-library/golang.git#294097713ad2eaf9ee4c5e9dc57e040ac4b402d6:1.25/alpine3.24","version_annotation":"1.25.12-alpine3.24","version_clues":{"GOLANG_VERSION":"1.25.12"}},"frontend-builder":{"input_reference":"docker.io/library/node:24.19.0-alpine3.23@sha256:244cc2b53f46f9e876304391d17682b0ddae9ac33491f4857e25e35a36ba7995","index_digest":"sha256:244cc2b53f46f9e876304391d17682b0ddae9ac33491f4857e25e35a36ba7995","pull_reference":"docker.io/library/node@sha256:5098ee834c9345ddd7fc2828a01dc90aa6de0e9ed6804a09a959b19a1fded97a","config_id":"sha256:4ce17b67701e49e0cbb99863724103ac3d49698e4d77454952f03319d9f6ae79","platform":{"os":"linux","architecture":"amd64","variant":null},"rootfs_diff_ids":["sha256:31ad4a471c6852bfec14d757cc75a566c82a9769f12c3918cf6bf52cc0eeb3d0","sha256:62c13154cc2df276b28734cd2bd96087f0cbc5fe51ec77d33edb742b6c7a7fd0","sha256:3047532ff6c25e499680e8088e4ea1380ee6e2356a6b5a34edd25158b0da0fe7","sha256:b6f42e1b6b7e968252f26352da8907b33e08048cd550575236c4507ff2f74ac7"],"source_annotation":"https://github.com/nodejs/docker-node.git#fd1bf45c51970427bc899084b5381f6827722246:24/alpine3.23","version_annotation":"24-alpine3.23","version_clues":{"NODE_VERSION":"24.19.0"}},"frontend-runtime":{"input_reference":"docker.io/library/nginx:1.30.3-alpine","index_digest":"sha256:0d3b80406a13a767339fbe2f41406d6c7da727ab89cf8fae399e81f780f814d1","pull_reference":"docker.io/library/nginx@sha256:61f816887864af764259e28edb5331584ae2e42de57b3cb1e4f134d757559951","config_id":"sha256:d0701bd41f8279135aa6c81e23fbcdae7bcc3df591c47741405412a8dcdb2eab","platform":{"os":"linux","architecture":"amd64","variant":null},"rootfs_diff_ids":["sha256:31ad4a471c6852bfec14d757cc75a566c82a9769f12c3918cf6bf52cc0eeb3d0","sha256:513bb45c86b3ab007a1f05b558211f5b69ae412d6834f4f890988a2f765fc399","sha256:e0292b802b8d62769f1e2f7ea969575894d0f7d8fb6244c824c95997591ebb5e","sha256:ccc7cae931833ad18af797b54e21afa361279bc2161e86ffe7eb89041cd6b962","sha256:c66f3fc2d00b6f8f7c46c9d94a23d9b51e66883151fc5edaccdf781e90df929f","sha256:0a7d4fc0fa12bb5b447649d680fc84a35167dde454e242fc843dbc670b208864","sha256:9c436f5312f14af26c4dd62076f77841be450178171276e676ed18744ab020bc","sha256:61d47f9d34fb476a81f30e840d869f301c2bde5b2b6878dd6577cbaf0b69842e"],"source_annotation":"https://github.com/nginx/docker-nginx.git#00348041cc12284266751457dcfc10b15645476a:stable/alpine","version_annotation":"1.30.3-alpine","version_clues":{"NGINX_VERSION":"1.30.3"}},"prometheus":{"input_reference":"docker.io/prom/prometheus:v2.51.0","index_digest":"sha256:5ccad477d0057e62a7cd1981ffcc43785ac10c5a35522dc207466ff7e7ec845f","pull_reference":"docker.io/prom/prometheus@sha256:3305cd2e483c13364c94ae4905368eb425c9fa804f8e523ee47d8bf4de9d87a2","config_id":"sha256:1d3b7f56885b6dd623f1785be963aa9c195f86bc256ea454e8d02a7980b79c53","platform":{"os":"linux","architecture":"amd64","variant":null},"rootfs_diff_ids":["sha256:1e604deea57dbda554a168861cff1238f93b8c6c69c863c43aed37d9d99c5fed","sha256:6b83872188a9e8912bee1d43add5e9bc518601b02a14a364c0da43b0d59acf33","sha256:85dc5d2fb7f3c2b212272d0b6dde4b4dd054fe198dcd65fbe45d3bb7a040ad35","sha256:1c9c778075e6faa2db9b56eaf0000ec4a0cee331446d8e3809004d220e153960","sha256:7d46235910cc6e7e5902434cdf6f648c4f2ab90d0ecf8d2c9691e382c60d4006","sha256:12bdb61bce406e005d782b45b7718cb02148e52982820de3b8504baf62516887","sha256:8125d70a13ea0729c0e4bf2911534e71d3ca09c7885c21153418ab831cd5002d","sha256:911d3bb42d7dc4fa365d72a732a3ea5e756105748c8e9c6dbe709ddb8750632a","sha256:75bd59ebad9fe3fee29b509e22a6152914d1d9789b6aa16cebe2da733430510f","sha256:723dceac4c2c56054cd6028304763a0b62b08f56f904440957dd3f2193431d03","sha256:96e04e6c7faacae1d6cd1822d0b98b8505e251557c7b98be0621498bfc8b7b42","sha256:f3d5dfc0e16ba96ada57fe9e97bf7f1b86aba7a1358ea60bf1ef478259ed28f5"],"source_annotation":null,"version_annotation":null,"version_clues":{}},"grafana":{"input_reference":"docker.io/grafana/grafana:11.0.0","index_digest":"sha256:0dc5a246ab16bb2c38a349fb588174e832b4c6c2db0981d0c3e6cd774ba66a54","pull_reference":"docker.io/grafana/grafana@sha256:a80bc3848cf5d4b2958ea25dbeb36fa9442ef4be8c73fe4bff11340307c32919","config_id":"sha256:f9095e2f0444d50d16f3d1a1bb52ca4a6971ba7359fe78e0f0247c3dc70047cc","platform":{"os":"linux","architecture":"amd64","variant":null},"rootfs_diff_ids":["sha256:d4fc045c9e3a848011de66f34b81f052d4f2c15a17bb196d637e526349601820","sha256:b17f3e80a64c0782eda08183a7892b03aaae5d441ab55b3d438078d97f65f3d4","sha256:c0399a717e254d6bc0ac6e9d3138d58b05a9b742bf37d95d39211fa445e194a6","sha256:3a6a55ef7f39a05ac540c8209eb8e04243efda4bad448e2edb1c56d83f3e0b4d","sha256:58580b8a4adc261a2ab93dfc80623cbf6837743d47780145c160dc7083e81328","sha256:af5724aa4d7b2da18ce75ede36f4697d0bc11ccaa27432110ecf534458855d6e","sha256:bb93c36776e477a9cd120b059f24de236df61c27adb2fb1cc554cacfe26991d6","sha256:a0d1afed4d21538be36f6bb646fa44b3d603f52c14cb9677325811142875d94e","sha256:d0a0a074a0d5d82c545d78a2be330ac363edc17d343b7c6abe2450b6198f6f3a","sha256:135a51aaf9d855c2961d666e0d7f9592d816b926c4a2e8c1c4bde40d56e939cb"],"source_annotation":null,"version_annotation":null,"version_clues":{}}}')
NOTICES = json.loads('[{"role":"frontend-runtime","path":"usr/share/licenses/nginx/COPYRIGHT","bytes":1377,"sha256":"4f72e2bccf0bcd839dd447ab6f359336f428caa1987a9afec2bc9ade5711658a","file":"notices/nginx-copyright.txt"},{"role":"frontend-runtime","path":"usr/share/licenses/nginx-module-njs/COPYRIGHT","bytes":1485,"sha256":"bf0746e75035cc2b9c3346feb08fd7aa414c088719c00e76734c6d7fe9c0647d","file":"notices/nginx-njs-copyright.txt"},{"role":"grafana","path":"usr/share/grafana/public/app/plugins/datasource/azuremonitor/dist/module.js.LICENSE.txt","bytes":246,"sha256":"96a525187855637cf799c57eb13c11119466d4a43b99a4a257af869bc45240d6","file":"notices/grafana-azuremonitor.LICENSE.txt"},{"role":"grafana","path":"usr/share/grafana/public/app/plugins/datasource/jaeger/dist/module.js.LICENSE.txt","bytes":311,"sha256":"d4d3d6875905570aa2e5f467c202f012e098e6f616d3a044842594a119d30702","file":"notices/grafana-jaeger.LICENSE.txt"},{"role":"prometheus","path":"npm_licenses.tar.bz2","bytes":139510,"sha256":"0501cce3982bdd4ec90928c29854bd19978f20089428008cb3bf477b0c8c2142","file":"notices/prometheus-npm-licenses.tar.bz2"}]')
TAG_COMMITS = json.loads('{"prometheus":"c05c15512acb675e3f6cd662a6727854e93fc024","grafana":"277ef258d4b9a5acdf2932347c6a4ca72d739b28"}')
OBSERVER_LIMITS = json.loads('{"modules":50000,"watchFiles":50000,"packages":2048,"distFiles":4096,"distBytes":67108864,"jsonBytes":16777216,"inputBytes":268435456,"edges":200000,"noticeFiles":8192,"noticeOne":1048576,"noticeTotal":8388608}')
CLAIM_LIMITS = json.loads('["A watched or parsed file is not necessarily emitted code.","Virtual modules, transformed CSS/assets and package notice completeness require disposition.","This is the newly observed build, not proof of old image bytes or of a future build."]')
# END FIXED INPUTS

MIB = 1024**2
MAX_DATA, MAX_ARCHIVE, MAX_TERMINAL, MAX_UPLOAD = 28*MIB, 30*MIB, 2*MIB, 32*MIB
MAX_FILES = 8192
MAX_JSON = 16*MIB
CODE = Path(__file__).resolve().parents[2]
REFERENCE = Path(__file__).with_name('community-image-candidate.py')
REFERENCE_SHA = '5b16c0d82afe20a731eb7ea822c1487d82717ab12f525ed3a1f1090d3e82f043'
ORIGINAL_LOCK_SHA = 'ad971525b5c57d2588456f1af9841f2b3f601d81eeebf404331ce46d4a277e14'
ORIGINAL_PACKAGE_SHA = '768d24bda4770b906fee9adea0cb0d5eb0168cea84f64ca812c7d9fbc2982cf5'
GAPS = {'notice_missing_or_mismatched', 'nested_archive_incomplete', 'binary_metadata_unavailable',
        'binary_source_partial_or_unbound', 'frontend_collection_failed', 'frontend_membership_requires_disposition',
        'collection_failed', 'cleanup_incomplete', 'delivery_rejected'}
PHASES = {'preflight','source','images','notices','binaries','frontend','cleanup','delivery'}
OPERATION_LABELS={'source producer checkout identity','source Docker environment','source commit','source tree',
    'export tracked source','source evidence exact stopped copy','source evidence one frontend builder',
    'source-frontend-builder image inspect','source evidence stopped frontend output','cleanup ownership',
    'stop owned tool process','remove owned container and anonymous volumes'}
OPERATION_LABELS|={r+s for r in IMAGES for s in [' pull',' image inspect']}
OPERATION_LABELS|={r+s for r in ['source-frontend-runtime','source-prometheus','source-grafana',
    'source-buildinfo-prometheus','source-buildinfo-grafana','source-frontend-output']
    for s in [' create',' container identity',' version/readback']}


class Rejected(Exception):
    pass


def need(value, code='SCHEMA'):
    if not value:
        raise Rejected(code)


def sha(body):
    return hashlib.sha256(body).hexdigest()


def utc():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()


def exact(value, keys):
    need(type(value) is dict and set(value) == set(keys))


def same_typed(value, expected):
    need(type(value) is type(expected))
    if type(expected) is dict:
        exact(value,expected)
        for key in expected:same_typed(value[key],expected[key])
    elif type(expected) is list:
        need(len(value)==len(expected))
        for a,b in zip(value,expected):same_typed(a,b)
    else:need(value==expected)


def text(value, cap=8192, nullable=False):
    if nullable and value is None:
        return
    need(type(value) is str and len(value.encode('utf-8')) <= cap)


def integer(value, cap=2**63-1):
    need(type(value) is int and 0 <= value <= cap)


def flag(value, nullable=False):
    need(type(value) is bool or (nullable and value is None))


def hexsha(value, nullable=False):
    need((nullable and value is None) or (type(value) is str and re.fullmatch('[0-9a-f]{64}', value)))


def rows(value, cap):
    need(type(value) is list and len(value) <= cap, 'LIMIT')


def relative_name(value):
    text(value, 1024)
    path = PurePosixPath(value)
    need(value and not path.is_absolute() and '..' not in path.parts and '\\' not in value
         and all(ord(c) >= 32 and ord(c) != 127 for c in value)
         and str(path) == value and value != '.', 'PATH')
    return value


def load_json(body, cap=MAX_JSON):
    need(len(body) <= cap, 'LIMIT')
    def unique(pairs):
        result = {}
        for key, value in pairs:
            need(key not in result)
            result[key] = value
        return result
    return json.loads(body.decode('utf-8'), object_pairs_hook=unique,
                      parse_constant=lambda _: (_ for _ in ()).throw(Rejected('SCHEMA')))


def regular(path, cap):
    info = path.lstat()
    need(stat.S_ISREG(info.st_mode) and info.st_nlink == 1 and info.st_size <= cap, 'FILE')
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(fd, 'rb') as stream:
        current = os.fstat(stream.fileno())
        need((current.st_dev,current.st_ino,current.st_size) == (info.st_dev,info.st_ino,info.st_size), 'FILE')
        body = stream.read(cap+1)
    need(len(body) == info.st_size and len(body) <= cap, 'LIMIT')
    return body


def write_new(path, body):
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, 'wb') as stream:
        stream.write(body)


def json_bytes(value, cap):
    body = (json.dumps(value, ensure_ascii=True, indent=2, allow_nan=False)+'\n').encode()
    need(len(body) <= cap, 'LIMIT')
    return body


class BoundedReader:
    def __init__(self, stream, cap, guard=lambda: None):
        self.stream, self.cap, self.guard, self.seen = stream, cap, guard, 0

    def read(self, count=-1):
        self.guard()
        need(self.seen < self.cap, 'LIMIT')
        count = 65536 if count < 0 else min(count, 65536)
        body = self.stream.read(min(count, self.cap-self.seen))
        self.seen += len(body)
        return body


def receive_single(stream, destination, basename, maximum, guard=lambda: None):
    seen = set()
    total = 0
    with tarfile.open(fileobj=BoundedReader(stream, maximum+MIB, guard), mode='r|') as archive:
        for member in archive:
            guard(); relative_name(member.name)
            need(member.name == basename and member.name not in seen and member.isfile(), 'TAR')
            seen.add(member.name)
            need(member.size <= maximum, 'LIMIT')
            with archive.extractfile(member) as inp:
                fd = os.open(destination, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
                with os.fdopen(fd, 'wb') as out:
                    while True:
                        guard(); body = inp.read(65536)
                        if not body: break
                        total += len(body); need(total <= member.size, 'LIMIT'); out.write(body)
            need(total == member.size, 'TAR')
    need(len(seen) == 1, 'TAR')
    return total


def receive_frontend(stream, destination, guard=lambda: None):
    seen = set(); count = total = 0
    with tarfile.open(fileobj=BoundedReader(stream, 24*MIB+MIB, guard), mode='r|') as archive:
        for member in archive:
            guard(); name = relative_name(member.name.rstrip('/') if member.isdir() else member.name)
            need(name not in seen, 'TAR'); seen.add(name)
            need(name == 'output' or name.startswith('output/'), 'PATH')
            if member.isdir():
                need(name in {'output','output/npm-notices'}, 'PATH'); continue
            need(member.isfile(), 'TAR')
            rel = name[len('output/'):]
            need(rel == 'frontend-membership.json' or re.fullmatch(r'npm-notices/[0-9]{4}-[A-Za-z0-9_.-]+', rel), 'PATH')
            count += 1; total += member.size
            need(count <= MAX_FILES and total <= 24*MIB, 'LIMIT')
            need(member.size <= (MAX_JSON if rel == 'frontend-membership.json' else MIB), 'LIMIT')
            path = destination/rel
            with archive.extractfile(member) as inp:
                body = inp.read(member.size+1)
            need(len(body) == member.size, 'TAR')
            write_new(path, body)
    need('output/frontend-membership.json' in seen, 'TAR')


def nested_index(raw, guard=lambda: None):
    result = []; seen = set(); total = 0; index_name_bytes=0
    reason=None
    try:
        with bz2.BZ2File(io.BytesIO(raw)) as expanded:
            with tarfile.open(fileobj=BoundedReader(expanded, 16*MIB+5*MIB, guard), mode='r|') as archive:
                for member in archive:
                    guard(); need(not PurePosixPath(member.name).is_absolute(),'PATH')
                    name=member.name.rstrip('/') if member.isdir() else member.name
                    while name.startswith('./'):name=name[2:]
                    if name in ('','.') and member.isdir():
                        need(member.name in {'.','./'},'PATH');name='.'
                    else:name=relative_name(name)
                    need(name not in seen,'DUPLICATE');need(len(seen)<4096,'LIMIT');seen.add(name)
                    need(member.isdir() or member.isfile(),'UNSUPPORTED_MEMBER')
                    if member.isdir(): continue
                    index_name_bytes+=len(json.dumps(name,ensure_ascii=True).encode())
                    need(index_name_bytes<=MIB,'LIMIT')
                    need(member.size <= MIB and total+member.size <= 16*MIB,'LIMIT')
                    with archive.extractfile(member) as inp:body=inp.read(member.size+1)
                    need(len(body)==member.size,'TAR')
                    total+=len(body);result.append({'path':name,'bytes':len(body),'sha256':sha(body)})
        need(result,'TAR')
    except Rejected as error:
        reason={'PATH':'unsafe_path','DUPLICATE':'duplicate_member','UNSUPPORTED_MEMBER':'unsupported_member',
                'LIMIT':'limit_exceeded','TAR':'invalid_archive'}.get(str(error),'invalid_archive')
    except (tarfile.TarError,OSError,EOFError,ValueError):reason='invalid_archive'
    return {'status':'incomplete' if reason else 'complete','archive_sha256':sha(raw),'reason':reason,
            'members':result,'regular_bytes':total,'entries_including_directories':len(seen)}


def validate_file_rows(value, count=MAX_FILES):
    rows(value,count); seen=set()
    for row in value:
        exact(row,['file','bytes','sha256']); relative_name(row['file'])
        need(row['file'] not in seen); seen.add(row['file'])
        integer(row['bytes'],64*MIB); hexsha(row['sha256'])
    return {x['file']:x for x in value}


def validate_membership(value):
    keys=['status','source_commit','actual_vite_version','actual_rollup_version',
          'original_source_lock_sha256','original_package_json_sha256',
          'post_install_package_lock_sha256','post_install_package_json_sha256','installed_hidden_lock_sha256',
          'plugin_order','graph','watch_inputs','emitted','output_files','public_copies',
          'unexplained_output_files','packages','limits','limits_of_claim']
    exact(value,keys)
    need(value['status']=='actual_build_graph_and_output_observations_not_license_clearance'
         and value['source_commit']==SOURCE['commit'])
    need(value['original_source_lock_sha256']==ORIGINAL_LOCK_SHA
         and value['original_package_json_sha256']==ORIGINAL_PACKAGE_SHA)
    for key in keys:
        if key.endswith('_sha256'): hexsha(value[key])
    for key in ['actual_vite_version','actual_rollup_version']: text(value[key],64)
    same_typed(value['limits'],OBSERVER_LIMITS)
    rows(value['plugin_order'],128)
    for item in value['plugin_order']: text(item,256)
    need(value['limits_of_claim']==CLAIM_LIMITS)
    gaps=set()
    if value['actual_vite_version']!='6.4.3' or value['actual_rollup_version']!='4.62.0': gaps.add('tool_version')
    rows(value['packages'],2048); packages={}; notice_files={}
    for package in value['packages']:
        exact(package,['location','name','version','declared_license','license_metadata_kind','unsupported_license_sha256',
                       'source_lock_version','installed_lock_version','exact_lock_version_match','reasons','notices'])
        loc=relative_name(package['location']); need('node_modules' in PurePosixPath(loc).parts and loc not in packages)
        packages[loc]=package
        for key in ['name','version','declared_license','source_lock_version','installed_lock_version']: text(package[key],4096,True)
        need(package['license_metadata_kind'] in {'missing','string','unsupported'})
        need((package['license_metadata_kind']=='string')==(package['declared_license'] is not None))
        hexsha(package['unsupported_license_sha256'],True)
        need((package['license_metadata_kind']=='unsupported')==(package['unsupported_license_sha256'] is not None))
        flag(package['exact_lock_version_match'])
        expected=bool(package['version']) and package['version']==package['source_lock_version']==package['installed_lock_version']
        need(package['exact_lock_version_match']==expected)
        rows(package['reasons'],4); need(package['reasons'] and len(set(package['reasons']))==len(package['reasons']))
        need(set(package['reasons']) <= {'rollup_graph','watch_input','emitted_chunk','build_tool_notice_candidate'})
        rows(package['notices'],8192)
        if not expected or not package['name'] or not package['notices'] or package['license_metadata_kind']=='unsupported': gaps.add('package_disposition')
        for item in package['notices']:
            exact(item,['file','source','bytes','sha256']); relative_name(item['file']); relative_name(item['source'])
            need(re.fullmatch(r'npm-notices/[0-9]{4}-[A-Za-z0-9_.-]+',item['file']) and item['source'].startswith(loc+'/'))
            need(item['file'] not in notice_files); notice_files[item['file']]=item
            integer(item['bytes'],MIB); hexsha(item['sha256'])
    need(len(notice_files)<=8192 and sum(x['bytes'] for x in notice_files.values())<=8*MIB,'LIMIT')
    def package_ref(value):
        text(value,1024,True); need(value is None or value in packages)
    rows(value['graph'],50000); graph_ids=set(); edges=0
    for module in value['graph']:
        exact(module,['id','package','info_present','included','external','imports','dynamic_imports'])
        text(module['id']); need(module['id'] not in graph_ids); graph_ids.add(module['id'])
        package_ref(module['package']); flag(module['info_present']); flag(module['included'],True); flag(module['external'],True)
        if not module['info_present'] or module['external'] or (module['id'].startswith('node_modules/') and module['package'] is None): gaps.add('graph_disposition')
        if module['id'].startswith('\x00') or not module['id']: gaps.add('virtual_disposition')
        for key in ['imports','dynamic_imports']:
            rows(module[key],200000); edges+=len(module[key])
            for item in module[key]: text(item)
    need(edges<=200000,'LIMIT')
    rows(value['watch_inputs'],50000)
    for item in value['watch_inputs']:
        exact(item,['id','package','sha256']); text(item['id']); package_ref(item['package']); hexsha(item['sha256'],True)
        if item['id'].startswith('node_modules/') and item['package'] is None: gaps.add('watch_disposition')
    output=validate_file_rows(value['output_files'],4096)
    need(sum(x['bytes'] for x in output.values())<=64*MIB,'LIMIT')
    public=validate_file_rows(value['public_copies'],4096)
    for key,row in public.items(): need(output.get(key)==row)
    unexplained=validate_file_rows(value['unexplained_output_files'],4096)
    rows(value['emitted'],4096); emitted=set()
    for item in value['emitted']:
        need(type(item) is dict and item.get('type') in {'chunk','asset'})
        relative_name(item['file']); need(item['file'] in output and item['file'] not in emitted); emitted.add(item['file'])
        if item['type']=='asset':
            exact(item,['type','file','original_files']); rows(item['original_files'],50000)
            for name in item['original_files']: text(name)
            if not item['original_files']: gaps.add('asset_disposition')
        else:
            exact(item,['type','file','imports','dynamic_imports','referenced_files','modules'])
            for key in ['imports','dynamic_imports','referenced_files']:
                rows(item[key],4096)
                for name in item[key]: text(name,1024)
            rows(item['modules'],50000)
            for module in item['modules']:
                exact(module,['id','package','rendered_length','original_length','rendered_exports_count','removed_exports_count'])
                text(module['id']); package_ref(module['package'])
                for key in ['rendered_length','original_length','rendered_exports_count','removed_exports_count']: integer(module[key])
                if module['id'] not in graph_ids or (module['package'] is None and module['id'].startswith('node_modules/')): gaps.add('chunk_disposition')
    need(set(unexplained)==set(output)-emitted-set(public))
    for key,row in unexplained.items(): need(output[key]==row)
    if unexplained: gaps.add('output_disposition')
    return notice_files, sorted(gaps)


def buildinfo_revision(body, role):
    need(len(body)<=256*1024,'LIMIT')
    text_value=body.decode('utf-8')
    candidates=[]; dirty_fields=[]; parse_errors=[]
    for line in text_value.splitlines():
        if '\tvcs.revision=' in line:
            candidates.append(line.split('vcs.revision=',1)[1])
        if '\tvcs.modified=' in line:
            dirty_fields.append(line.split('vcs.modified=',1)[1])
        if '-ldflags=' in line:
            try:
                words=shlex.split(line.split('-ldflags=',1)[1])
                if len(words)==1 and ' ' in words[0]: words=shlex.split(words[0])
            except ValueError:
                parse_errors.append('invalid_ldflags');continue
            for token in words:
                if token.startswith('-X='):token=token[3:]
                elif token.startswith('-X') and token!='-X':token=token[2:]
                if token in {'main.commit','github.com/prometheus/common/version.Revision'}:
                    candidates.append('');continue
                if '=' not in token:continue
                name,value=token.split('=',1)
                if name in {'main.commit','github.com/prometheus/common/version.Revision'}: candidates.append(value)
    rows(candidates,64);rows(dirty_fields,64)
    for item in candidates+dirty_fields:text(item,4096)
    valid=sorted(set(x for x in candidates if re.fullmatch('[0-9a-f]{7,40}',x)))
    dirty_values=set(dirty_fields)
    dirty=next(iter(dirty_values)) if len(dirty_values)==1 and dirty_values<={'true','false'} else None
    expected=TAG_COMMITS[role]
    match='unbound'
    if (parse_errors or not dirty_values<={'true','false'} or len(dirty_values)>1 or dirty=='true' or not valid
            or len(valid)!=len(set(candidates)) or any(not expected.startswith(x) for x in valid)):match='unbound'
    elif valid==[expected]: match='full_embedded_revision_match' if dirty=='false' else 'full_revision_cleanliness_unknown'
    else: match='short_embedded_revision_only'
    return {'revision_declarations':candidates,'dirty_declarations':dirty_fields,'parse_errors':parse_errors,
            'revisions':valid,'dirty':dirty,'comparison':match}


def load_reference(path=REFERENCE):
    raw=regular(path,128*1024);need(sha(raw)==REFERENCE_SHA,'REFERENCE')
    # Compile the bytes just checked; do not permit an unrelated cached pyc.
    guard=types.ModuleType('ur_accepted_source_guards');guard.__file__=str(path.resolve())
    exec(compile(raw,str(path.resolve()),'exec'),guard.__dict__)
    need(guard.FIXED['source']==SOURCE and {k:guard.FIXED['images'][k] for k in IMAGES}==IMAGES,'REFERENCE')
    return guard


def all_regular(root, allowed_directories):
    need(root.is_dir() and not root.is_symlink(),'FILE')
    result=[]
    for path in sorted(root.rglob('*')):
        info=path.lstat()
        need(not stat.S_ISLNK(info.st_mode),'FILE')
        if stat.S_ISDIR(info.st_mode):
            need(path.relative_to(root).as_posix() in allowed_directories,'PATH');continue
        relative_name(path.relative_to(root).as_posix())
        need(stat.S_ISREG(info.st_mode) and info.st_nlink==1,'FILE'); result.append(path)
        need(len(result)<=MAX_FILES,'LIMIT')
    return result


def validate_evidence(files):
    """files is a bounded map of relative names to exact raw bytes."""
    need(type(files) is dict and len(files)<=MAX_FILES and sum(map(len,files.values()))<=MAX_DATA,'LIMIT')
    need(all(type(body) is bytes for body in files.values()))
    for name in files: relative_name(name)
    fixed={x['file']:x for x in NOTICES}
    extra={'binaries/prometheus.buildinfo.txt','binaries/grafana.buildinfo.txt',
           'frontend/frontend-membership.json','notices/prometheus-npm-index.json'}
    for name,body in files.items():
        if name in fixed:
            row=fixed[name]; need((len(body),sha(body))==(row['bytes'],row['sha256']),'IDENTITY')
        elif name.startswith('frontend/npm-notices/'):
            need(re.fullmatch(r'frontend/npm-notices/[0-9]{4}-[A-Za-z0-9_.-]+',name) and len(body)<=MIB,'PATH')
        else: need(name in extra,'PATH')
    if 'notices/prometheus-npm-licenses.tar.bz2' in files:
        expected=nested_index(files['notices/prometheus-npm-licenses.tar.bz2'])
        need('notices/prometheus-npm-index.json' in files)
        need(load_json(files['notices/prometheus-npm-index.json'],2*MIB)==expected,'IDENTITY')
    else: need('notices/prometheus-npm-index.json' not in files)
    member_name='frontend/frontend-membership.json'
    npm={name for name in files if name.startswith('frontend/npm-notices/')}
    if member_name in files:
        refs,_=validate_membership(load_json(files[member_name]))
        need(npm=={'frontend/'+name for name in refs})
        for name,row in refs.items(): need((len(files['frontend/'+name]),sha(files['frontend/'+name]))==(row['bytes'],row['sha256']),'IDENTITY')
    else: need(not npm)
    for role in TAG_COMMITS:
        name='binaries/'+role+'.buildinfo.txt'
        if name in files: buildinfo_revision(files[name],role)


def file_cap(name):
    for row in NOTICES:
        if name==row['file']:return row['bytes']
    if name in {'binaries/prometheus.buildinfo.txt','binaries/grafana.buildinfo.txt'}:return 256*1024
    if name=='frontend/frontend-membership.json':return MAX_JSON
    if name=='notices/prometheus-npm-index.json':return 2*MIB
    if re.fullmatch(r'frontend/npm-notices/[0-9]{4}-[A-Za-z0-9_.-]+',name):return MIB
    raise Rejected('PATH')


def package_evidence(data, destination):
    paths=all_regular(data,{'notices','binaries','frontend','frontend/npm-notices'})
    need(sum(p.stat().st_size for p in paths)<=MAX_DATA,'LIMIT')
    files={p.relative_to(data).as_posix():regular(p,file_cap(p.relative_to(data).as_posix())) for p in paths}
    validate_evidence(files)
    manifest={'files':[{'file':name,'bytes':len(body),'sha256':sha(body)} for name,body in sorted(files.items())]}
    manifest_body=json_bytes(manifest,2*MIB)
    need(sum(map(len,files.values()))+len(manifest_body)<=MAX_DATA,'LIMIT')
    with destination.open('xb') as raw:
        with tarfile.open(fileobj=raw,mode='w:gz',format=tarfile.USTAR_FORMAT) as archive:
            for name,body in [('evidence-manifest.json',manifest_body),*sorted(files.items())]:
                item=tarfile.TarInfo(name); item.size=len(body); item.mode=0o600; item.mtime=0
                archive.addfile(item,io.BytesIO(body))
                need(raw.tell()<=MAX_ARCHIVE,'LIMIT')
    need(destination.stat().st_size<=MAX_ARCHIVE,'LIMIT')
    return {'file':'source-evidence.tar.gz','bytes':destination.stat().st_size,'sha256':sha(regular(destination,MAX_ARCHIVE)),
            'manifest_sha256':sha(manifest_body),'members':manifest['files']}


def validate_archive(body, expected):
    need(len(body)<=MAX_ARCHIVE and (len(body),sha(body))==(expected['bytes'],expected['sha256']),'IDENTITY')
    files={}; manifest=None; expanded=0
    with gzip.GzipFile(fileobj=io.BytesIO(body)) as decompressed:
        with tarfile.open(fileobj=BoundedReader(decompressed,MAX_DATA+8*MIB),mode='r|') as archive:
            for item in archive:
                relative_name(item.name); need(item.isfile() and item.name not in files,'TAR')
                cap=2*MIB if item.name=='evidence-manifest.json' else file_cap(item.name)
                need(len(files)<MAX_FILES+1 and item.size<=cap,'LIMIT')
                expanded+=item.size; need(expanded<=MAX_DATA,'LIMIT')
                with archive.extractfile(item) as inp: raw=inp.read(item.size+1)
                need(len(raw)==item.size,'TAR'); files[item.name]=raw
    need('evidence-manifest.json' in files,'TAR')
    raw=files.pop('evidence-manifest.json'); need(sha(raw)==expected['manifest_sha256'],'IDENTITY')
    manifest=load_json(raw,2*MIB); exact(manifest,['files']); listed=validate_file_rows(manifest['files'])
    need(manifest['files']==expected['members'] and set(listed)==set(files))
    for name,entry in listed.items(): need((len(files[name]),sha(files[name]))==(entry['bytes'],entry['sha256']),'IDENTITY')
    validate_evidence(files)
    return files


def validate_terminal(value):
    exact(value,['schema','status','run','expected_source','observed_source','images','notices','binaries','frontend','operations',
                 'cleanup','gaps','failure','bundle','producer','authority'])
    need(value['schema']=='ur-source-evidence-v1' and value['status'] in {'collected','partial','failed'})
    run=value['run']; exact(run,['id','attempt','workflow_commit','started_utc','ended_utc'])
    need(type(run['id']) is str and re.fullmatch('[0-9]{1,24}',run['id']) and type(run['attempt']) is int and run['attempt']==1)
    need(type(run['workflow_commit']) is str and re.fullmatch('[0-9a-f]{40}',run['workflow_commit']))
    for key in ['started_utc','ended_utc']:
        text(run[key],64); need(datetime.datetime.fromisoformat(run[key]).utcoffset()==datetime.timedelta(0))
    need(datetime.datetime.fromisoformat(run['ended_utc'])>=datetime.datetime.fromisoformat(run['started_utc']))
    same_typed(value['expected_source'],SOURCE)
    source=value['observed_source']
    if source is not None:
        exact(source,['commit','tree','files','regular_bytes','archive_sha256'])
        integer(source['files']);integer(source['regular_bytes'])
        need(source['commit']==SOURCE['commit'] and source['tree']==SOURCE['tree']
             and source['files']==SOURCE['tracked_files'] and source['regular_bytes']==SOURCE['tracked_regular_bytes'])
        hexsha(source['archive_sha256'])
    need(type(value['images']) is dict and set(value['images'])<=set(IMAGES))
    for role,image in value['images'].items():
        same_typed(image,{key:IMAGES[role][key] for key in ['pull_reference','config_id','rootfs_diff_ids']})
    rows(value['notices'],5)
    need(len(value['notices'])==5)
    for row,fixed in zip(value['notices'],NOTICES):
        exact(row,['file','status','bytes','sha256'])
        need(row['file']==fixed['file'] and row['status'] in {'not_observed','matched','gap'})
        integer(row['bytes'],MIB); hexsha(row['sha256'],True)
        if row['status']=='matched': need((row['bytes'],row['sha256'])==(fixed['bytes'],fixed['sha256']))
    need(type(value['binaries']) is dict and set(value['binaries'])<=set(TAG_COMMITS))
    for role,item in value['binaries'].items():
        exact(item,['bytes','sha256','metadata_file','revision_declarations','dirty_declarations','parse_errors','revisions','dirty','comparison'])
        integer(item['bytes'],384*MIB); hexsha(item['sha256'])
        need(item['metadata_file']=='binaries/'+role+'.buildinfo.txt')
        rows(item['revisions'],64)
        for key in ['revision_declarations','dirty_declarations']:
            rows(item[key],64)
            for declaration in item[key]:text(declaration,4096)
        rows(item['parse_errors'],64);need(set(item['parse_errors'])<={'invalid_ldflags'})
        for revision in item['revisions']: need(type(revision) is str and re.fullmatch('[0-9a-f]{7,40}',revision))
        need(item['dirty'] in {None,'true','false'} and item['comparison'] in {'unbound','short_embedded_revision_only','full_embedded_revision_match','full_revision_cleanliness_unknown'})
    need(sum(x['bytes'] for x in value['binaries'].values())<=512*MIB,'LIMIT')
    f=value['frontend']; exact(f,['status','image_id','membership_sha256','disposition'])
    need(f['status'] in {'not_observed','observed','gap'}); hexsha(f['membership_sha256'],True)
    need(f['image_id'] is None or (type(f['image_id']) is str and re.fullmatch('sha256:[0-9a-f]{64}',f['image_id'])))
    need((f['status']=='observed')==(f['membership_sha256'] is not None))
    if f['status']=='observed':need(f['image_id'] is not None)
    rows(f['disposition'],16)
    need(set(f['disposition'])<={'tool_version','package_disposition','graph_disposition','virtual_disposition','watch_disposition','asset_disposition','chunk_disposition','output_disposition'})
    rows(value['cleanup'],32)
    need(len({x['id'] for x in value['cleanup']})==len(value['cleanup']))
    for item in value['cleanup']:
        exact(item,['id','removed']); hexsha(item['id']); flag(item['removed'])
    rows(value['operations'],128)
    for item in value['operations']:
        exact(item,['phase','status','exit_code','started_utc','ended_utc','log_bytes','log_sha256','receiver_complete'])
        need(item['phase'] in OPERATION_LABELS and item['status'] in {'success','failed','timeout'})
        need(item['exit_code'] is None or (type(item['exit_code']) is int and -128<=item['exit_code']<=255))
        for key in ['started_utc','ended_utc']:
            text(item[key],64);need(datetime.datetime.fromisoformat(item[key]).utcoffset()==datetime.timedelta(0))
        if item['log_bytes'] is not None:integer(item['log_bytes'],2*MIB)
        hexsha(item['log_sha256'],True);flag(item['receiver_complete'])
        if item['status']=='success':need(item['exit_code']==0 and item['receiver_complete'] and item['log_sha256'] is not None)
    rows(value['gaps'],len(GAPS)); need(len(set(value['gaps']))==len(value['gaps']) and set(value['gaps'])<=GAPS)
    if value['failure'] is not None:
        exact(value['failure'],['phase','code']); need(value['failure']['phase'] in PHASES and value['failure']['code'] in {'collection_failed','cleanup_incomplete','delivery_rejected'})
    if value['bundle'] is not None:
        b=value['bundle']; exact(b,['file','bytes','sha256','manifest_sha256','members'])
        need(b['file']=='source-evidence.tar.gz'); integer(b['bytes'],MAX_ARCHIVE); hexsha(b['sha256']); hexsha(b['manifest_sha256']); validate_file_rows(b['members'])
    exact(value['producer'],['helper_sha256','observer_sha256','reference_sha256','derived_Dockerfile_sha256'])
    for key,data in value['producer'].items(): hexsha(data,nullable=key=='derived_Dockerfile_sha256')
    need(value['producer']['reference_sha256']==REFERENCE_SHA)
    same_typed(value['authority'],{'binary_source_proof':'build_declared_only','complete_license_clearance':False,
                                  'formal_artifact':False,'public_install_verified':False})
    if value['status']=='collected':
        need(not value['gaps'] and value['failure'] is None and source is not None and set(value['images'])==set(IMAGES)
             and all(x['status']=='matched' for x in value['notices']) and set(value['binaries'])==set(TAG_COMMITS)
             and all(x['comparison']=='full_embedded_revision_match' for x in value['binaries'].values())
             and f['status']=='observed' and not f['disposition'] and all(x['removed'] for x in value['cleanup'])
             and value['bundle'] is not None)


def validate_delivery(directory):
    need(directory.is_dir() and not directory.is_symlink(),'FILE')
    names={path.name for path in directory.iterdir()}
    need(names in [{'terminal-source-evidence.json'},{'terminal-source-evidence.json','source-evidence.tar.gz'}],'PATH')
    raw=regular(directory/'terminal-source-evidence.json',MAX_TERMINAL)
    value=load_json(raw,MAX_TERMINAL); validate_terminal(value)
    if value['bundle'] is None:
        need(names=={'terminal-source-evidence.json'})
        return 'UR_SOURCE_TERMINAL_VALID'
    need(names=={'terminal-source-evidence.json','source-evidence.tar.gz'})
    archive=regular(directory/'source-evidence.tar.gz',MAX_ARCHIVE)
    need(len(raw)+len(archive)<=MAX_UPLOAD,'LIMIT')
    files=validate_archive(archive,value['bundle'])
    for item in value['notices']:
        need((item['status']=='matched')==(item['file'] in files))
    if 'notices/prometheus-npm-index.json' in files:
        incomplete=load_json(files['notices/prometheus-npm-index.json'])['status']=='incomplete'
        need(incomplete==('nested_archive_incomplete' in value['gaps']))
    for role,item in value['binaries'].items():
        need(item['metadata_file'] in files)
        observed=buildinfo_revision(files[item['metadata_file']],role)
        need(all(item[key]==observed[key] for key in observed))
    need({name for name in files if name.startswith('binaries/')}=={x['metadata_file'] for x in value['binaries'].values()})
    member='frontend/frontend-membership.json'
    need((value['frontend']['status']=='observed')==(member in files))
    if member in files:
        need(sha(files[member])==value['frontend']['membership_sha256'])
        _,disposition=validate_membership(load_json(files[member])); need(disposition==value['frontend']['disposition'])
    return 'UR_SOURCE_BUNDLE_VALID'


def configure(guard):
    need(sys.version_info>=(3,10),'ENVIRONMENT')
    required={'GITHUB_REPOSITORY':'foigent/unirouter','GITHUB_EVENT_NAME':'workflow_dispatch',
              'GITHUB_REF':'refs/heads/main','RUNNER_ENVIRONMENT':'github-hosted','RUNNER_OS':'Linux',
              'RUNNER_ARCH':'X64','GITHUB_RUN_ATTEMPT':'1'}
    need(all(os.environ.get(k)==v for k,v in required.items()) and platform.machine()=='x86_64','ENVIRONMENT')
    expected=os.environ.get('EXPECTED_WORKFLOW_COMMIT',''); run_id=os.environ.get('GITHUB_RUN_ID','')
    need(re.fullmatch('[0-9a-f]{40}',expected) and expected==os.environ.get('GITHUB_SHA') and re.fullmatch('[0-9]{1,24}',run_id),'ENVIRONMENT')
    need(not any(os.environ.get(k) for k in ['DOCKER_HOST','DOCKER_CONTEXT','DOCKER_TLS_VERIFY','DOCKER_CERT_PATH','BUILDKIT_HOST','BUILDX_BUILDER']),'ENVIRONMENT')
    start=os.environ.get('UR_SOURCE_JOB_MONOTONIC',''); need(re.fullmatch(r'[0-9]+(?:\.[0-9]+)?',start),'ENVIRONMENT')
    start=float(start); need(0<start<=time.monotonic(),'ENVIRONMENT')
    guard.run_id=run_id; guard.deadline=start+2280; guard.cleanup_deadline=start+2400
    guard.time_guard()
    need(Path(os.environ['GITHUB_WORKSPACE']).resolve()==CODE and guard.CODE==CODE,'ENVIRONMENT')
    base=Path(os.environ['RUNNER_TEMP']).resolve(); need(base.is_dir(),'ENVIRONMENT')
    guard.work=base/('ur-source-evidence-'+run_id+'-attempt1'); guard.work.mkdir(mode=0o700)
    for name in ['scratch','data','deliverable','docker-config']:(guard.work/name).mkdir(mode=0o700)
    write_new(guard.work/'docker-config/config.json',b'{"auths":{}}\n')
    guard.child_env={'PATH':os.environ['PATH'],'LANG':'C.UTF-8','LC_ALL':'C.UTF-8','TZ':'UTC',
                     'DOCKER_CONFIG':str(guard.work/'docker-config'),'DOCKER_BUILDKIT':'1','TMPDIR':str(guard.work),
                     'GIT_CONFIG_NOSYSTEM':'1','GIT_CONFIG_GLOBAL':'/dev/null','GIT_ATTR_NOSYSTEM':'1'}
    guard.receipt['status']='source_evidence_running'
    return expected,run_id


def initial_terminal(expected,run_id):
    return {'schema':'ur-source-evidence-v1','status':'failed',
            'run':{'id':run_id,'attempt':1,'workflow_commit':expected,'started_utc':utc(),'ended_utc':utc()},
            'expected_source':SOURCE,'observed_source':None,'images':{},
            'notices':[{'file':x['file'],'status':'not_observed','bytes':0,'sha256':None} for x in NOTICES],
            'binaries':{},'frontend':{'status':'not_observed','image_id':None,'membership_sha256':None,'disposition':[]},
            'cleanup':[],'operations':[],'gaps':[],'failure':None,'bundle':None,
            'producer':{'helper_sha256':sha(Path(__file__).read_bytes()),
                        'observer_sha256':sha(Path(__file__).with_name('frontend-membership-observer.mjs').read_bytes()),
                        'reference_sha256':REFERENCE_SHA,'derived_Dockerfile_sha256':None},
            'authority':{'binary_source_proof':'build_declared_only','complete_license_clearance':False,
                         'formal_artifact':False,'public_install_verified':False}}


def copy_exact(guard,cid,path,destination,maximum):
    guard.stream_command('source evidence exact stopped copy',['docker','--config',str(guard.work/'docker-config'),
                         '--host','unix:///var/run/docker.sock','cp',cid+':/'+path,'-'],
                         lambda stream:receive_single(stream,destination,PurePosixPath(path).name,maximum,guard.time_guard))
    guard.disk_guard()


def outside_volume(guard,role,path):
    metadata=next(x for x in guard.receipt['owned_containers'] if x['role']=='source-'+role)
    need(not any('/'+path==v or ('/'+path).startswith(v.rstrip('/')+'/') for v in metadata['declared_volumes']),'PATH')


def observe(guard,result):
    phase='preflight'; gaps=set()
    try:
        need(guard.argv_run('source producer checkout identity',['git','-C',str(CODE),'rev-parse','HEAD'])==result['run']['workflow_commit'],'IDENTITY')
        event=load_json(Path(os.environ['GITHUB_EVENT_PATH']).read_bytes(),2*MIB)
        need(event['repository']['full_name']=='foigent/unirouter' and event['repository']['id']==1281228065,'IDENTITY')
        info=load_json(guard.docker('source Docker environment','info','--format',
          '{"OSType":{{json .OSType}},"Architecture":{{json .Architecture}},"DockerRootDir":{{json .DockerRootDir}},"SecurityOptions":{{json .SecurityOptions}}}').encode(),MIB)
        need(info['OSType']=='linux' and info['Architecture'] in ['x86_64','amd64'] and not any('rootless' in x for x in info['SecurityOptions']),'ENVIRONMENT')
        guard.docker_root=Path(info['DockerRootDir']); need(guard.docker_root.is_absolute() and guard.docker_root.is_dir(),'ENVIRONMENT')
        guard.disk_guard(initial=True)
        phase='source'; source=guard.source_export(); result['observed_source']=guard.receipt['source_export']
        phase='images'; ids={}
        for role in IMAGES:
            ids[role]=guard.pull(role)
            result['images'][role]={key:IMAGES[role][key] for key in ['pull_reference','config_id','rootfs_diff_ids']}
        containers={role:guard.create('source-'+role,ids[role],['--network','none','--read-only','--cap-drop','ALL','--security-opt','no-new-privileges']) for role in ['frontend-runtime','prometheus','grafana']}
        for row in guard.receipt['owned_containers']:
            need(not row['started'] and not any(x.get('Type')=='bind' for x in row['mounts']),'ENVIRONMENT')
        phase='notices'
        for fixed,entry in zip(NOTICES,result['notices']):
            scratch=guard.work/'scratch'/('notice-'+str(NOTICES.index(fixed)))
            try:
                outside_volume(guard,fixed['role'],fixed['path'])
                copy_exact(guard,containers[fixed['role']],fixed['path'],scratch,MIB)
                raw=regular(scratch,MIB); entry.update(bytes=len(raw),sha256=sha(raw))
                need((len(raw),sha(raw))==(fixed['bytes'],fixed['sha256']),'IDENTITY')
                write_new(guard.work/'data'/fixed['file'],raw); entry['status']='matched'
                if fixed['file'].endswith('.tar.bz2'):
                    index=nested_index(raw,guard.time_guard)
                    if index['status']=='incomplete':gaps.add('nested_archive_incomplete')
                    write_new(guard.work/'data/notices/prometheus-npm-index.json',json_bytes(index,2*MIB))
            except Exception:
                if entry['status']!='matched':entry['status']='gap'
                gaps.add('notice_missing_or_mismatched'); guard.time_guard()
        phase='binaries'; binary_total=0
        for role,path in [('prometheus','bin/prometheus'),('grafana','usr/share/grafana/bin/grafana')]:
            try:
                binary=guard.work/'scratch'/('binary-'+role)
                outside_volume(guard,role,path)
                maximum=min(384*MIB,512*MIB-binary_total); need(maximum>0,'LIMIT')
                copy_exact(guard,containers[role],path,binary,maximum); binary_total+=binary.stat().st_size
                binary.chmod(0o644)
                binary_sha=guard.hash_file(binary)
                old_limit=guard.MAX_LOG_BYTES; guard.MAX_LOG_BYTES=256*1024
                try:guard.tool('source-buildinfo-'+role,ids['backend-builder'],
                    'mkdir -p /tmp/telemetry; printf "off\\n" > /tmp/telemetry/mode; go version -m /inspect/uniroute',binary)
                finally:guard.MAX_LOG_BYTES=old_limit
                raw=regular(guard.work/(str(len(guard.receipt['commands']))+'.command.log'),256*1024)
                revision=buildinfo_revision(raw,role)
                name='binaries/'+role+'.buildinfo.txt'; write_new(guard.work/'data'/name,raw)
                result['binaries'][role]={'bytes':binary.stat().st_size,'sha256':binary_sha,'metadata_file':name,**revision}
                if revision['comparison']!='full_embedded_revision_match':gaps.add('binary_source_partial_or_unbound')
            except Exception:
                gaps.add('binary_metadata_unavailable'); guard.time_guard()
        phase='frontend'
        try:collect_frontend(guard,result,source,gaps)
        except Exception:
            result['frontend']['status']='gap'; gaps.add('frontend_collection_failed'); guard.time_guard()
    except Exception:
        gaps.add('collection_failed'); result['failure']={'phase':phase,'code':'collection_failed'}
    finally:
        try:guard.cleanup()
        except Exception:gaps.add('cleanup_incomplete')
        result['cleanup']=[{'id':x['id'],'removed':x['removed_with_anonymous_volumes']} for x in guard.receipt['cleanup']]
        result['operations']=[{'phase':x['phase'],'status':x['status'],'exit_code':x.get('exit_code'),
            'started_utc':x['started_utc'],'ended_utc':x['ended_utc'],'log_bytes':x.get('log_bytes'),
            'log_sha256':x.get('log_sha256'),'receiver_complete':x.get('log_receiver_complete',False)}
            for x in guard.receipt['commands']]
        if len(result['cleanup'])!=len(guard.owned) or not all(x['removed'] for x in result['cleanup']):gaps.add('cleanup_incomplete')
        result['gaps']=sorted(gaps)


def collect_frontend(guard,result,source,gaps):
    root=source/'frontend'; instrument=root/'.ur-source-evidence'; instrument.mkdir(mode=0o700)
    observer=regular(Path(__file__).with_name('frontend-membership-observer.mjs'),128*1024)
    write_new(instrument/'membership-observer.mjs',observer)
    write_new(instrument/'original-package.json',regular(root/'package.json',MIB))
    write_new(instrument/'original-package-lock.json',regular(root/'package-lock.json',MIB))
    wrapper=b"import { mergeConfig } from 'vite';\nimport original from '../vite.config';\nimport { membershipObserver } from './membership-observer.mjs';\nexport default mergeConfig(original, { plugins: [membershipObserver()] });\n"
    write_new(instrument/'vite.config.ts',wrapper)
    original=regular(root/'Dockerfile',65536).decode()
    need(sha(original.encode())=='4b755b7f1f1f8857527ce8969c64c92053eb9be15c22a665636773a6a91689df','IDENTITY')
    prefix=original.split('# Production stage',1)[0]; need(prefix.count('RUN npm run build')==1,'IDENTITY')
    command='RUN npm run check:toolchain && ./node_modules/.bin/tsc && npm run check:api-contracts && npm run check:react-compat && ./node_modules/.bin/vite build --config .ur-source-evidence/vite.config.ts && node scripts/check-bundle-budget.mjs'
    derived=prefix.replace('RUN npm run build',command).encode(); dockerfile=guard.work/'frontend-builder.Dockerfile'; write_new(dockerfile,derived)
    result['producer']['derived_Dockerfile_sha256']=sha(derived)
    guard.docker('source evidence one frontend builder','build','--platform','linux/amd64','--target','builder','--file',str(dockerfile),
                 '--build-arg','VITE_DEPLOY_ENV=production','--build-arg','VITE_APP_VERSION=1.14.0',
                 '--tag','ur-source-evidence-frontend-builder:'+guard.run_id,'--iidfile',str(guard.work/'frontend-builder.id'),str(root))
    iid=regular(guard.work/'frontend-builder.id',128).decode().strip(); need(re.fullmatch('sha256:[0-9a-f]{64}',iid),'IDENTITY')
    image=guard.inspect_image('source-frontend-builder',iid)
    need(image==iid and guard.receipt['images']['source-frontend-builder']['rootfs_diff_ids'][:len(IMAGES['frontend-builder']['rootfs_diff_ids'])]==IMAGES['frontend-builder']['rootfs_diff_ids'],'IDENTITY')
    cid=guard.create('source-frontend-output',iid,['--network','none','--read-only','--cap-drop','ALL','--security-opt','no-new-privileges'])
    scratch=guard.work/'scratch/frontend'; scratch.mkdir()
    guard.stream_command('source evidence stopped frontend output',['docker','--config',str(guard.work/'docker-config'),
        '--host','unix:///var/run/docker.sock','cp',cid+':/app/.ur-source-evidence/output','-'],lambda stream:receive_frontend(stream,scratch,guard.time_guard))
    files={'frontend/'+p.relative_to(scratch).as_posix():regular(p,MAX_JSON) for p in all_regular(scratch,{'npm-notices'})}
    validate_evidence(files)
    membership=files['frontend/frontend-membership.json']; _,disposition=validate_membership(load_json(membership))
    for name,raw in files.items():guard.disk_guard(); write_new(guard.work/'data'/name,raw)
    result['frontend']={'status':'observed','image_id':iid,'membership_sha256':sha(membership),'disposition':disposition}
    if disposition:gaps.add('frontend_membership_requires_disposition')


def finish(guard,result):
    delivered=guard.work/'deliverable'; staging=guard.work/'source-evidence.tar.gz'
    try:
        guard.disk_guard()
        if all_regular(guard.work/'data',{'notices','binaries','frontend','frontend/npm-notices'}):
            result['bundle']=package_evidence(guard.work/'data',staging)
            need(staging.stat().st_size+MAX_TERMINAL<=MAX_UPLOAD,'LIMIT')
        else:result['bundle']=None
    except Exception:
        result['bundle']=None
        result['gaps']=sorted(set(result['gaps'])|{'delivery_rejected'})
        result['failure']=result['failure'] or {'phase':'delivery','code':'delivery_rejected'}
    result['status']='failed' if result['failure'] else 'partial' if result['gaps'] else 'collected'
    result['run']['ended_utc']=utc(); validate_terminal(result)
    terminal=json_bytes(result,MAX_TERMINAL)
    if result['bundle'] is not None:
        os.rename(staging,delivered/'source-evidence.tar.gz')
    write_new(delivered/'terminal-source-evidence.json',terminal)
    validate_delivery(delivered)
    return 0 if result['status']=='collected' else 1


def main():
    if len(sys.argv)==3 and sys.argv[1]=='--validate-delivery':
        try:print(validate_delivery(Path(sys.argv[2]))); return 0
        except Exception:print('UR_SOURCE_VALIDATION_REJECTED',file=sys.stderr); return 2
    need(len(sys.argv)==1,'ARGUMENTS')
    guard=load_reference(); expected,run_id=configure(guard)
    result=initial_terminal(expected,run_id)
    observe(guard,result)
    return finish(guard,result)


if __name__=='__main__':
    try:raise SystemExit(main())
    except Exception:print('UR_SOURCE_COLLECTION_STOPPED',file=sys.stderr); raise SystemExit(1)
