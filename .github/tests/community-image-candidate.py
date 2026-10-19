#!/usr/bin/env python3
"""Fixed hosted build facts only. No image/archive publication or installation."""
import base64
import datetime
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import platform
import re
import shutil
import signal
import stat
import subprocess
import sys
import tarfile
import threading
import time

FIXED = json.loads('{"source":{"repository":"foigent/unirouter","commit":"3815a584e78161b475a22d5f477a66d6869984c3","tree":"345ee7691a208c32254438404f9c0baabe27aa07","version":"1.13.0","schema":"000127_xinsight_communication_policy","tracked_files":2981,"tracked_regular_bytes":50616501},"accepted_batch_a":{"sha256":"88d2c7f78f0ca98b4fc9520e0e2c2810f37a93ceb6953736c9cd7c3c899fbf37","acceptance_sha256":"bd1625f3f836e07012d3e4d00a8e24f957393be9716484383c3e0268a5c799b1"},"approved_public_ring":{"key_id":"ur-community-upgrade-20261011-a","bytes":83,"sha256":"96c91635460ddfb2a14aa1ea97d8445951730e9eb04a33cbdf4320141e61d63d","decoded_key_sha256":"765f38f8a4a4f38bf7e94aa3236c28f74dccac56431ab3a1d035a880f6086116"},"source_files":{"Dockerfile":{"sha256":"18bf189862b4e3b67b4c6c5d5f8e82d8a02416e5c2dc76734fcdcd3e3f165bb3","bytes":2970},"frontend/Dockerfile":{"sha256":"4b755b7f1f1f8857527ce8969c64c92053eb9be15c22a665636773a6a91689df","bytes":698},"go.mod":{"sha256":"3dc95bb094fd2a4493c0b382950b2d3f963d0be4171ef72a62c2d33d279b86ef","bytes":2697},"go.sum":{"sha256":"73beb67d43e7537b310a242e4aed034f3bfd4c27754b93d836bd546c310b26e4","bytes":11449},"frontend/package.json":{"sha256":"6f7e936114d219e60ef7a3e8fcb975cf3b8d2137989ab9695e12e7fcc9e0523f","bytes":1695},"frontend/package-lock.json":{"sha256":"0bb1d4d9a9fd6d76bd2d90a0cdd2ae74c7486e61c45453c2bea967d7862a3efa","bytes":284976},"frontend/scripts/check-toolchain.mjs":{"sha256":"046d4315f9cf2566430799eb808cfba0906f1941b70b8033f3e3bc67f6f2704c","bytes":938},".dockerignore":{"sha256":"2e504f58b13ce72d4651dc17c588de0ed8ec1a92792ebc829038b66fe01b8a09","bytes":661},"frontend/.dockerignore":{"sha256":"6d8e7df9cabafa86b2295c2da4db18d78062d721f3799e7b89ea390774c04a6d","bytes":394},"migrations/000126_commercial_license_retirement.up.sql":{"sha256":"4893743e65125538b9f2d3a46c9aafbb7d8d54ecab4264eaa9afc92bb16ec73a","bytes":5713},"migrations/000127_xinsight_communication_policy.up.sql":{"sha256":"1eb7fd6426743424e92f7f98d84773afebcdda589a2df19e61f3557dd3fd2707","bytes":9119}},"images":{"backend-builder":{"input_reference":"docker.io/library/golang:1.25.12-alpine","index_digest":"sha256:56961d79ea8129efddcc0b8643fd8a5416b4e6228cfd477e3fd61deb2672c587","pull_reference":"docker.io/library/golang@sha256:2b6edeb8c6b1071bfa16473f24bb7b7da0b1579009f97bb1542f239b14aabd8f","config_id":"sha256:c7d080ccd6695ac0b69509d2ca5686ddc405b27019ea1452221307deb3c76bd8","platform":{"os":"linux","architecture":"amd64","variant":null},"rootfs_diff_ids":["sha256:34884abbe92863fce933ed7c39c0e045631af0ed86d5cc0dfbdf9fdca426ce3c","sha256:49c5f4bd4150345d6bdfa7c7e677d73fe03cc742598901e5ac40959d9a3072f2","sha256:c11523315a2840edc8c35a91f1f9f475a1da16bed3de0eae0f03a674873b475a","sha256:c91b45a2b070045f87ad6b195cdef95f77cef782df535b0bf4d91d0606548325","sha256:5f70bf18a086007016e948b04aed3b82103a36bea41755b6cddfaf10ace3c6ef"],"source_annotation":"https://github.com/docker-library/golang.git#294097713ad2eaf9ee4c5e9dc57e040ac4b402d6:1.25/alpine3.24","version_annotation":"1.25.12-alpine3.24","version_clues":{"GOLANG_VERSION":"1.25.12"}},"backend-runtime":{"input_reference":"docker.io/library/alpine:3.23","index_digest":"sha256:85fe1e81d6758c208f3e1eed4338a1997e19d4be002d4dd32d3100c9a8c010a0","pull_reference":"docker.io/library/alpine@sha256:1f3591b8a02ea153f41c5bba878ad477f63ab3d19349762cb77504db02a23e15","config_id":"sha256:f6d31b983274a75183386cda3aac54849498fac0fc3e0d98f92ff56ed517d92b","platform":{"os":"linux","architecture":"amd64","variant":null},"rootfs_diff_ids":["sha256:e63b02c2b5c761df2cb95e657d744f27d95da4e051235a13c80b88cd68eab188"],"source_annotation":"https://github.com/alpinelinux/docker-alpine.git#00d3407329e4c65f237cc1e325a4ab0cb4974df4:x86_64","version_annotation":"3.23.6","version_clues":{}},"frontend-builder":{"input_reference":"docker.io/library/node:24.19.0-alpine3.23@sha256:244cc2b53f46f9e876304391d17682b0ddae9ac33491f4857e25e35a36ba7995","index_digest":"sha256:244cc2b53f46f9e876304391d17682b0ddae9ac33491f4857e25e35a36ba7995","pull_reference":"docker.io/library/node@sha256:5098ee834c9345ddd7fc2828a01dc90aa6de0e9ed6804a09a959b19a1fded97a","config_id":"sha256:4ce17b67701e49e0cbb99863724103ac3d49698e4d77454952f03319d9f6ae79","platform":{"os":"linux","architecture":"amd64","variant":null},"rootfs_diff_ids":["sha256:31ad4a471c6852bfec14d757cc75a566c82a9769f12c3918cf6bf52cc0eeb3d0","sha256:62c13154cc2df276b28734cd2bd96087f0cbc5fe51ec77d33edb742b6c7a7fd0","sha256:3047532ff6c25e499680e8088e4ea1380ee6e2356a6b5a34edd25158b0da0fe7","sha256:b6f42e1b6b7e968252f26352da8907b33e08048cd550575236c4507ff2f74ac7"],"source_annotation":"https://github.com/nodejs/docker-node.git#fd1bf45c51970427bc899084b5381f6827722246:24/alpine3.23","version_annotation":"24-alpine3.23","version_clues":{"NODE_VERSION":"24.19.0"}},"frontend-runtime":{"input_reference":"docker.io/library/nginx:1.30.3-alpine","index_digest":"sha256:0d3b80406a13a767339fbe2f41406d6c7da727ab89cf8fae399e81f780f814d1","pull_reference":"docker.io/library/nginx@sha256:61f816887864af764259e28edb5331584ae2e42de57b3cb1e4f134d757559951","config_id":"sha256:d0701bd41f8279135aa6c81e23fbcdae7bcc3df591c47741405412a8dcdb2eab","platform":{"os":"linux","architecture":"amd64","variant":null},"rootfs_diff_ids":["sha256:31ad4a471c6852bfec14d757cc75a566c82a9769f12c3918cf6bf52cc0eeb3d0","sha256:513bb45c86b3ab007a1f05b558211f5b69ae412d6834f4f890988a2f765fc399","sha256:e0292b802b8d62769f1e2f7ea969575894d0f7d8fb6244c824c95997591ebb5e","sha256:ccc7cae931833ad18af797b54e21afa361279bc2161e86ffe7eb89041cd6b962","sha256:c66f3fc2d00b6f8f7c46c9d94a23d9b51e66883151fc5edaccdf781e90df929f","sha256:0a7d4fc0fa12bb5b447649d680fc84a35167dde454e242fc843dbc670b208864","sha256:9c436f5312f14af26c4dd62076f77841be450178171276e676ed18744ab020bc","sha256:61d47f9d34fb476a81f30e840d869f301c2bde5b2b6878dd6577cbaf0b69842e"],"source_annotation":"https://github.com/nginx/docker-nginx.git#00348041cc12284266751457dcfc10b15645476a:stable/alpine","version_annotation":"1.30.3-alpine","version_clues":{"NGINX_VERSION":"1.30.3"}},"postgres":{"input_reference":"docker.io/library/postgres:16-alpine","index_digest":"sha256:721873c34ceb9f8d8fc265984940dc982404c105f19ad51be9fdc5970a6080ea","pull_reference":"docker.io/library/postgres@sha256:1a66d744c1b459e13b05a8fca341da84cb63383e99ce262210efee5a319d4551","config_id":"sha256:81bd698b4594e751a3269e4dcd3e03a4a0ec0daf7b72e7aa1abd43cce9887542","platform":{"os":"linux","architecture":"amd64","variant":null},"rootfs_diff_ids":["sha256:74d97c428c51a828f9051a7a40a53ff1fc99e54fc30323ce36760701b0b7f711","sha256:55469241be312953a965a95ac8affc80a9e6caca7ae6b7c865eeb85bed6c0935","sha256:7efd3fa04051c098ad166c177d3b5df5db8019f3b230a11650cae3b99e0247d4","sha256:6abee75ee30a340e7bb44aba0ebc89f77573822aaae42ee12c0cd8ac003d0dca","sha256:1e6eba70f825671214a5a7f9b042c272b510657980522e2972deee9745685660","sha256:09bef825aa08dbb0073c2ffd188ea973f42d09ac910a0c88545825f958f70a8f","sha256:470ae8e9f008967fa132e4f35c6b7d5dd6f74aac23c2eacdb45d82dd2d1f12e4","sha256:b006494f482847c06687d7f95f7be1683af2e7545b951f81f20041b5e0644ebb","sha256:ed38a2de685f56abc00e510ad09c7ddf5fe342bc545cfb1f7e64a4b6e2f46b05","sha256:3a1fd3ebad6c8e69047f4b2c1e4cd78d37425ee3bbbb72642a3ad9ba00731b33","sha256:38dab51118192bc13c247cd7d9356d34b50c2eb436337b2e251dd14d2e9be5bb"],"source_annotation":"https://github.com/docker-library/postgres.git#9d15534160ade17f2b6c455a39ee967c49b1937d:16/alpine3.24","version_annotation":"16.15-alpine3.24","version_clues":{"PG_MAJOR":"16","PG_VERSION":"16.15"}},"redis":{"input_reference":"docker.io/library/redis:7-alpine","index_digest":"sha256:858f009f9709ce576febc734aa78b8f6d624b82571f9ddb6bda4377c833b3499","pull_reference":"docker.io/library/redis@sha256:ca0acbb137c1dc3339c8b147a58fd6f42775d4599327b50e7b116c23de501af2","config_id":"sha256:f84b0c4678011602b9b98c227a4dcd5468bf8b088b02fdd4165cb7758bad8058","platform":{"os":"linux","architecture":"amd64","variant":null},"rootfs_diff_ids":["sha256:1073f425247ed24670458429c13a67afbd36f83d2004dade046ddbd6519b1839","sha256:d20c6b7f7fb37ca551ba319c057ff51c0aca5885ff6a11b3930273c7529800ca","sha256:32624b38a78645bf62112555038b3b1cac8416b165585ea0e4cc2adba947caa2","sha256:b2d2118710fee1efc64b70aba5f4214311003fa76e708711e25f8256be2f31c9","sha256:ff386fc941ca6f6ef64f2571918c36bae96d4fd1e9fcd7190d61bfed14d66d5b","sha256:5f70bf18a086007016e948b04aed3b82103a36bea41755b6cddfaf10ace3c6ef","sha256:88266afbda8cd921017b37ccfefaa0e3782dfeb3e5bc854a0101e4273ddeb475"],"source_annotation":"https://github.com/redis/docker-library-redis.git#74654c612ee06275377d483dc4e134e57b463e9e:alpine","version_annotation":"7.4.11-alpine","version_clues":{"REDIS_VERSION":"7.4.11"}},"prometheus":{"input_reference":"docker.io/prom/prometheus:v2.51.0","index_digest":"sha256:5ccad477d0057e62a7cd1981ffcc43785ac10c5a35522dc207466ff7e7ec845f","pull_reference":"docker.io/prom/prometheus@sha256:3305cd2e483c13364c94ae4905368eb425c9fa804f8e523ee47d8bf4de9d87a2","config_id":"sha256:1d3b7f56885b6dd623f1785be963aa9c195f86bc256ea454e8d02a7980b79c53","platform":{"os":"linux","architecture":"amd64","variant":null},"rootfs_diff_ids":["sha256:1e604deea57dbda554a168861cff1238f93b8c6c69c863c43aed37d9d99c5fed","sha256:6b83872188a9e8912bee1d43add5e9bc518601b02a14a364c0da43b0d59acf33","sha256:85dc5d2fb7f3c2b212272d0b6dde4b4dd054fe198dcd65fbe45d3bb7a040ad35","sha256:1c9c778075e6faa2db9b56eaf0000ec4a0cee331446d8e3809004d220e153960","sha256:7d46235910cc6e7e5902434cdf6f648c4f2ab90d0ecf8d2c9691e382c60d4006","sha256:12bdb61bce406e005d782b45b7718cb02148e52982820de3b8504baf62516887","sha256:8125d70a13ea0729c0e4bf2911534e71d3ca09c7885c21153418ab831cd5002d","sha256:911d3bb42d7dc4fa365d72a732a3ea5e756105748c8e9c6dbe709ddb8750632a","sha256:75bd59ebad9fe3fee29b509e22a6152914d1d9789b6aa16cebe2da733430510f","sha256:723dceac4c2c56054cd6028304763a0b62b08f56f904440957dd3f2193431d03","sha256:96e04e6c7faacae1d6cd1822d0b98b8505e251557c7b98be0621498bfc8b7b42","sha256:f3d5dfc0e16ba96ada57fe9e97bf7f1b86aba7a1358ea60bf1ef478259ed28f5"],"source_annotation":null,"version_annotation":null,"version_clues":{}},"grafana":{"input_reference":"docker.io/grafana/grafana:11.0.0","index_digest":"sha256:0dc5a246ab16bb2c38a349fb588174e832b4c6c2db0981d0c3e6cd774ba66a54","pull_reference":"docker.io/grafana/grafana@sha256:a80bc3848cf5d4b2958ea25dbeb36fa9442ef4be8c73fe4bff11340307c32919","config_id":"sha256:f9095e2f0444d50d16f3d1a1bb52ca4a6971ba7359fe78e0f0247c3dc70047cc","platform":{"os":"linux","architecture":"amd64","variant":null},"rootfs_diff_ids":["sha256:d4fc045c9e3a848011de66f34b81f052d4f2c15a17bb196d637e526349601820","sha256:b17f3e80a64c0782eda08183a7892b03aaae5d441ab55b3d438078d97f65f3d4","sha256:c0399a717e254d6bc0ac6e9d3138d58b05a9b742bf37d95d39211fa445e194a6","sha256:3a6a55ef7f39a05ac540c8209eb8e04243efda4bad448e2edb1c56d83f3e0b4d","sha256:58580b8a4adc261a2ab93dfc80623cbf6837743d47780145c160dc7083e81328","sha256:af5724aa4d7b2da18ce75ede36f4697d0bc11ccaa27432110ecf534458855d6e","sha256:bb93c36776e477a9cd120b059f24de236df61c27adb2fb1cc554cacfe26991d6","sha256:a0d1afed4d21538be36f6bb646fa44b3d603f52c14cb9677325811142875d94e","sha256:d0a0a074a0d5d82c545d78a2be330ac363edc17d343b7c6abe2450b6198f6f3a","sha256:135a51aaf9d855c2961d666e0d7f9592d816b926c4a2e8c1c4bde40d56e939cb"],"source_annotation":null,"version_annotation":null,"version_clues":{}}},"declared_unique_compressed_layer_bytes":505731483,"not_bound":{"npm_version":"11.17.0 must be probed; not proven by A","prometheus_grafana_image_source_revision":null,"workflow_commit":null,"run_or_environment":null,"current_ci_cost_or_budget_headroom":null,"application_registry_digests":null}}')
MAX_REPORT = 1024 * 1024
FACTS_FILENAME = "facts-report.json"
MAX_LOG_BYTES = 2 * 1024 * 1024  # Per command, outside the uploaded output allowlist.
DIAGNOSTIC_TAIL_BYTES = 8192
INITIAL_FREE = 12 * 1024**3
MIN_FREE = 2 * 1024**3
HELPER_SECONDS = 38 * 60  # The 45-minute job also needs checkout/upload time.
CODE = Path(__file__).resolve().parents[2]
SOURCE_CHECKOUT = CODE / '.image-source-3815a584'
work = delivered = pending = docker_root = None
child_env = None
deadline = None
cleanup_deadline = None
run_id = None
owned = []
receipt = {'status':'not_started', 'source':FIXED['source'],
           'accepted_batch_a':FIXED['accepted_batch_a'], 'commands':[],
           'images':{}, 'files':{}, 'owned_containers':[], 'cleanup':[],
           'unresolved':['Image notice/component/source-delivery review is not complete.',
                         'Frontend bundle component membership is not a complete SBOM.',
                         'Prometheus/Grafana exact image source revisions remain unbound.',
                         'Application registry digests, install.json and signed release are absent.'],
           'boundaries':{'publish_or_push':False,'real_signing_keys':False,
                         'ur_database_monitoring_service_started':False,
                         'synthetic_installer_or_model_test':False}}

class Stop(Exception):
    pass

class CommandTimeout(Stop):
    pass

def utc():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()

def digest(b):
    return hashlib.sha256(b).hexdigest()

def require(ok, message):
    if not ok: raise Stop(message)

def diagnostic(site,error,stage=None):
    try:
        if site not in ('main','cleanup','report','module','validate_report'): return
        stage=receipt.get('stage') if stage is None else stage
        if type(stage) is not str or stage not in STAGES: stage='unknown'
        name=type(error).__name__
        if name not in {'Stop','CommandTimeout','TimeoutExpired','OSError','ValueError',
                        'KeyError','TypeError','JSONDecodeError','FileNotFoundError',
                        'PermissionError','RuntimeError','BrokenPipeError','AttributeError',
                        'AssertionError'}: name='OtherError'
        arg=error.args[0] if type(error) is Stop and len(error.args)==1 else None
        codes={'INVALID_ARGUMENTS','REPORT_COMMAND_REJECTED','REPORT_COMPLETION_REJECTED',
               'REPORT_GAPS_REJECTED','REPORT_IMAGE_REJECTED','REPORT_INPUT_REJECTED',
               'REPORT_MODULE_REJECTED','REPORT_PATH_REJECTED','REPORT_SCHEMA_REJECTED',
               'REPORT_SIZE_REJECTED','REPORT_SOURCE_REJECTED','REPORT_TRUST_REJECTED',
               'REPORT_VALUE_REJECTED'}
        code=arg if type(arg) is str and len(arg)<=64 and arg in codes else 'unknown'
        line=None; trace=error.__traceback__
        while trace is not None:
            frame=trace.tb_frame.f_code
            if frame.co_filename==__file__ and frame.co_name not in {
                    'require','exact','text_field','integer','flag','hex_digest','rows'}:
                line=trace.tb_lineno
            trace=trace.tb_next
        print(json.dumps({'diagnostic':'image_facts_failure','site':site,'stage':stage,
                          'class':name,'code':code,'line':line},
                         ensure_ascii=True).replace('#',r'\u0023'),file=sys.stderr,flush=True)
    except Exception:
        pass  # Diagnostics must never replace the failure being reported.

def apk_field_witness(role,image,inventory,row_index,field,value):
    if role not in ('backend','frontend','postgres','redis','prometheus','grafana'): return
    if re.fullmatch(r'sha256:[0-9a-f]{64}',image['id']) is None: return
    if role in FIXED['images'] and image['id']!=FIXED['images'][role]['config_id']: return
    databases=[row for row in inventory['databases'] if row['path']=='lib/apk/db/installed']
    if len(databases)!=1 or re.fullmatch(r'[0-9a-f]{64}',databases[0]['sha256']) is None: return
    if field not in ('name','version','declared_license','origin'): return
    if value is None:
        if field in ('declared_license','origin'): return
        reason='missing_required'
    elif type(value) is not str: reason='wrong_type'
    elif not value: reason='empty'
    elif len(value)>256: reason='too_long'
    elif re.fullmatch(r'[A-Za-z0-9_.+():/ -]+',value) is None: reason='invalid_characters'
    else: return
    kind={type(None):'null',str:'string',bool:'boolean',int:'integer',float:'number',
          list:'array',dict:'object'}.get(type(value),'other')
    raw=value.encode('utf-8') if type(value) is str else None
    complete=value is None or (raw is not None and len(raw)<=4096)
    event={'diagnostic':'apk_field_rejected','role':role,'image_id':image['id'],
           'database_sha256':databases[0]['sha256'],'row_index':row_index,'field':field,
           'code':'REPORT_VALUE_REJECTED','reason':reason,'value_type':kind,
           'value_characters':len(value) if type(value) is str else None,
           'value_utf8_bytes':len(raw) if raw is not None else None,
           'value_sha256':hashlib.sha256(raw).hexdigest() if raw is not None else None,
           'value_base64':base64.b64encode(raw).decode('ascii') if raw is not None and complete else None,
           'value_complete':complete}
    line=json.dumps(event,ensure_ascii=True,separators=(',',':'),allow_nan=False).replace('#',r'\u0023')
    if len(line)+1<=8192: print(line,file=sys.stderr,flush=True)

def hash_file(p):
    h=hashlib.sha256()
    with p.open('rb') as f:
        while True:
            b=f.read(65536)
            if not b: break
            h.update(b)
    return h.hexdigest()

def disk_guard(initial=False):
    minimum=INITIAL_FREE if initial else MIN_FREE
    for p in [work,docker_root]:
        if p is not None:
            require(shutil.disk_usage(p).free>=minimum,'insufficient task or DockerRootDir disk')

def time_guard():
    require(deadline is None or time.monotonic()<deadline,'helper deadline reached')

def snapshot():
    # Internal runner-only state. The workflow never uploads this file.
    if work is None: return
    raw=(json.dumps(receipt,indent=2)+'\n').encode()
    require(len(raw)<=MAX_REPORT,'internal facts capacity exceeded')
    p=work/'internal-receipt.json'; p.write_bytes(raw); os.chmod(p,0o600)

class CommandLog:
    """Drain only this command's log pipe; stop it before an unbounded log write."""
    def __init__(self,process,pipe,file):
        self.process=process; self.pipe=pipe; self.file=file
        self.tail=b''; self.seen=0; self.limit_exceeded=False; self.error_class=None
        self.thread=threading.Thread(target=self.drain,daemon=True); self.thread.start()
    def drain(self):
        try:
            while True:
                b=self.pipe.read1(min(65536,MAX_LOG_BYTES-self.seen+1))
                if not b: break
                self.tail=(self.tail+b)[-DIAGNOSTIC_TAIL_BYTES:]
                allowed=MAX_LOG_BYTES-self.seen; self.seen+=len(b)
                self.file.write(b[:allowed]); self.file.flush()
                if self.seen>MAX_LOG_BYTES:
                    self.limit_exceeded=True
                    try: self.process.kill()
                    except ProcessLookupError: pass
                    break
        except Exception as e:
            self.error_class=type(e).__name__
            if self.process.poll() is None:
                try: self.process.kill()
                except ProcessLookupError: pass
        finally: self.pipe.close()

def run_command(label,args,consume=None,cleanup=False):
    if not cleanup: time_guard(); disk_guard()
    remaining=min(15,cleanup_deadline-time.monotonic()) if cleanup else deadline-time.monotonic()
    require(remaining>0,'command time reserve exhausted')
    row={'phase':label,'argv':args,'started_utc':utc(),'status':'running','exit_code':None}
    receipt['commands'].append(row); snapshot()
    log=work/(str(len(receipt['commands']))+('.stream.log' if consume else '.command.log'))
    p=capture=previous=None
    try: log_file=log.open('xb')
    except Exception as e:
        row.update(status='failed',error_class=type(e).__name__,ended_utc=utc(),
                   failure_reason='command log could not be opened',process_not_started=True,
                   log_bytes=None,log_sha256=None,
                   failure_diagnostic={'channel':'stderr' if consume else 'combined stdout/stderr',
                                       'content_unavailable_reason':'command_log_open_failed',
                                       'truncated':False,'raw_log_uploaded':False})
        snapshot(); raise
    with log_file as f:
        try:
            end=time.monotonic()+remaining
            p=subprocess.Popen(args,stdout=subprocess.PIPE,stderr=subprocess.PIPE if consume else subprocess.STDOUT,env=child_env)
            capture=CommandLog(p,p.stderr if consume else p.stdout,f)
            if consume:
                def expired(signum,frame): raise CommandTimeout('stream exceeded helper deadline')
                previous=signal.signal(signal.SIGALRM,expired)
                signal.setitimer(signal.ITIMER_REAL,max(.1,end-time.monotonic()))
                consume(p.stdout)
            p.wait(timeout=max(.1,end-time.monotonic()))
            capture.thread.join(timeout=max(0,min(2,end-time.monotonic())))
            require(not capture.thread.is_alive(),'command log receiver incomplete')
            require(not capture.limit_exceeded,'command log limit exceeded')
            require(capture.error_class is None,'command log receiver failed')
            require(p.returncode==0,'command failed: '+label)
            row['status']='success'
        except Exception as e:
            row.update(status='timeout' if isinstance(e,(CommandTimeout,subprocess.TimeoutExpired)) else 'failed',error_class=type(e).__name__)
            row['failure_reason']='command deadline reached' if row['status']=='timeout' else str(e) if isinstance(e,Stop) else 'command or stream failure; see error class and filtered excerpt'
            raise
        finally:
            if previous is not None:
                signal.setitimer(signal.ITIMER_REAL,0); signal.signal(signal.SIGALRM,previous)
            if p is not None:
                if p.poll() is None:
                    try: p.kill()
                    except ProcessLookupError: pass
                    reap=max(0,min(5,cleanup_deadline-time.monotonic()))
                    if reap:
                        try: p.wait(timeout=reap)
                        except subprocess.TimeoutExpired: row['process_exit_unconfirmed']=True
                row['exit_code']=p.poll()
                if row['exit_code'] is None: row['process_exit_unconfirmed']=True
                if consume: p.stdout.close()
            if capture:
                capture.thread.join(timeout=max(0,min(2,cleanup_deadline-time.monotonic())))
                row.update(log_limit_exceeded=capture.limit_exceeded,log_receiver_error_class=capture.error_class,
                           log_receiver_complete=not capture.thread.is_alive())
            if row['status']=='running': row.update(status='failed',error_class='InterruptedCommand')
            f.flush()
            row.update(ended_utc=utc(),log_bytes=log.stat().st_size,
                       log_sha256=hash_file(log) if capture is None or not capture.thread.is_alive() else None)
            snapshot()
            try:
                if (label in ('one release backend build', 'one production frontend build')
                        and row['status'] != 'success' and capture is not None
                        and row.get('log_receiver_complete')):
                    print(json.dumps({
                        'build_failure_phase': label, 'exit_code': row.get('exit_code'),
                        'output_tail': capture.tail.decode('utf-8', errors='replace'),
                        'truncated': capture.seen > len(capture.tail),
                    }, ensure_ascii=True).replace('#', r'\u0023'), flush=True)
            except Exception:
                # Diagnostic output must not replace the primary command failure.
                pass
    return log.read_text(errors='replace').strip() if consume is None else None

def argv_run(label,args,cleanup=False):
    return run_command(label,args,cleanup=cleanup)

def docker(label,*args,cleanup=False):
    return argv_run(label,['docker','--config',str(work/'docker-config'),
                           '--host','unix:///var/run/docker.sock',*args],cleanup)

def inspect_image(role,ref,expected=None):
    data=json.loads(docker(role+' image inspect','image','inspect',ref))[0]
    require(data['Os']=='linux' and data['Architecture']=='amd64','image platform mismatch')
    if expected:
        require(data['Id']==expected['config_id'],'pulled config identity mismatch')
        require(data['RootFS']['Layers']==expected['rootfs_diff_ids'],'pulled rootfs identity mismatch')
        matches=[]
        for r in data.get('RepoDigests') or []:
            repo,sep,dg=r.partition('@')
            if not sep or dg!=expected['pull_reference'].split('@')[1]: continue
            parts=repo.split('/')
            if parts[0] in ['docker.io','index.docker.io','registry-1.docker.io']: parts=parts[1:]
            if len(parts)==1: parts=['library',parts[0]]
            canonical='docker.io/'+'/'.join(parts)
            if canonical==expected['pull_reference'].split('@')[0]: matches.append(r)
        require(bool(matches),'expected repository/digest not returned by Docker')
        actual_pull_reference=sorted(matches)[0]
    else: actual_pull_reference=None
    receipt['images'][role]={'id':data['Id'],'platform':{'os':data['Os'],'arch':data['Architecture']},
                             'rootfs_diff_ids':data['RootFS']['Layers'],'repo_digests':data.get('RepoDigests') or [],
                             'exact_installer_pull_candidate':actual_pull_reference,
                             'declared_volumes':sorted((data['Config'].get('Volumes') or {}).keys()),
                             'registry_digest_unbound':expected is None}
    snapshot(); return data['Id']

def pull(role):
    data=FIXED['images'][role]
    docker(role+' pull','pull','--platform','linux/amd64',data['pull_reference'])
    return inspect_image(role,data['pull_reference'],data)

def create(role,image_id,extra=(),command=()):
    name='ur-batchb-'+run_id+'-'+role
    cid=docker(role+' create','create','--name',name,'--label','ur.batchb.run='+run_id,
               *extra,image_id,*command)
    require(bool(re.fullmatch('[0-9a-f]{64}',cid)),'unexpected container identity')
    owned.append(cid)
    meta=json.loads(docker(role+' container identity','container','inspect',cid))[0]
    require(meta['Id']==cid and meta['Config']['Labels'].get('ur.batchb.run')==run_id,'container ownership mismatch')
    receipt['owned_containers'].append({'id':cid,'name':name,'role':role,'mounts':meta['Mounts'],
                                         'declared_volumes':meta['Config'].get('Volumes') or {},'started':False})
    snapshot(); return cid

def tool(role,image_id,script,mount=None):
    args=['--network','none','--read-only','--cap-drop','ALL','--security-opt','no-new-privileges',
          '--tmpfs','/tmp:rw,noexec,nosuid,size=32m','--entrypoint','/bin/sh',
          '--env','GOTOOLCHAIN=local','--env','GOENV=off','--env','GOWORK=off',
          '--env','GOPROXY=off','--env','GOSUMDB=off','--env','TEST_TELEMETRY_DIR=/tmp/telemetry',
          '--env','NPM_CONFIG_UPDATE_NOTIFIER=false','--env','NPM_CONFIG_CACHE=/tmp/npm-cache']
    if mount:
        require(mount.is_relative_to(work) and ',' not in str(mount),'invalid task mount')
        args += ['--mount','type=bind,src='+str(mount)+',dst=/inspect/uniroute,readonly']
    cid=create(role,image_id,args,('-c',script))
    next(x for x in receipt['owned_containers'] if x['id']==cid)['tool_start_requested']=True
    snapshot()
    out=docker(role+' version/readback','start','--attach',cid)
    next(x for x in receipt['owned_containers'] if x['id']==cid)['started']=True
    snapshot(); return out

def source_export():
    commit=argv_run('source commit',['git','-C',str(SOURCE_CHECKOUT),'rev-parse','HEAD'])
    tree=argv_run('source tree',['git','-C',str(SOURCE_CHECKOUT),'rev-parse','HEAD^{tree}'])
    require(commit==FIXED['source']['commit'] and tree==FIXED['source']['tree'],'product source drift')
    archive=work/'source.tar'
    argv_run('export tracked source',['git','-C',str(SOURCE_CHECKOUT),'archive','--format=tar','--output='+str(archive),commit])
    target=work/'source'; target.mkdir(mode=0o700); count=total=0
    with tarfile.open(archive,'r:') as tf:
        for m in tf:
            path=PurePosixPath(m.name)
            require(not path.is_absolute() and '..' not in path.parts and '\x00' not in m.name,'unsafe source member')
            dest=target.joinpath(*path.parts)
            if m.isdir(): dest.mkdir(parents=True,exist_ok=True); continue
            require(m.isfile(),'nonregular source member')
            dest.parent.mkdir(parents=True,exist_ok=True)
            with tf.extractfile(m) as inp, dest.open('xb') as out: shutil.copyfileobj(inp,out,65536)
            os.chmod(dest,0o755 if m.mode&0o111 else 0o644)
            count+=1; total+=m.size
    require(count==FIXED['source']['tracked_files'] and total==FIXED['source']['tracked_regular_bytes'],'source export size/set drift')
    for rel,b in FIXED['source_files'].items():
        f=target/rel; require(f.stat().st_size==b['bytes'] and hash_file(f)==b['sha256'],'source input mismatch: '+rel)
    receipt['source_export']={'commit':commit,'tree':tree,'files':count,'regular_bytes':total,'archive_sha256':hash_file(archive)}
    snapshot(); return target

def recipes(source):
    back=(source/'Dockerfile').read_text(); front=(source/'frontend/Dockerfile').read_text()
    edits=[('FROM golang:1.25.12-alpine AS builder','FROM '+FIXED['images']['backend-builder']['pull_reference']+' AS builder'),
           ('FROM alpine:3.23','FROM '+FIXED['images']['backend-runtime']['pull_reference'])]
    for old,new in edits:
        require(back.splitlines().count(old)==1,'backend FROM contract drift'); back=back.replace(old,new,1)
    old='FROM nginx:1.30.3-alpine'; new='FROM '+FIXED['images']['frontend-runtime']['pull_reference']
    require(front.splitlines().count(old)==1,'frontend FROM contract drift'); front=front.replace(old,new,1)
    node_line='FROM node:24.19.0-alpine3.23@'+FIXED['images']['frontend-builder']['index_digest']+' AS builder'
    require(front.splitlines().count(node_line)==1,'fixed Node FROM changed')
    b=work/'backend.Dockerfile'; f=work/'frontend.Dockerfile'; b.write_text(back); f.write_text(front)
    receipt['recipe_derivation']={'backend_sha256':hash_file(b),'frontend_sha256':hash_file(f),
                                   'changes':'Only two backend FROM references and frontend nginx FROM; Node index unchanged.'}
    snapshot(); return b,f

def stream_command(label,args,consume):
    return run_command(label,args,consume=consume)

def apk_packages(b):
    result=[]
    for block in b.decode('utf-8',errors='replace').split('\n\n'):
        d=dict(line.split(':',1) for line in block.splitlines() if ':' in line)
        if d.get('P'): result.append({k:d.get(v) for k,v in [('name','P'),('version','V'),('license','L'),('origin','o'),('upstream_url','U')]})
    return result

def inventory(role,image_id):
    cid=create(role+'-inventory',image_id)
    databases={}; notices=[]
    stats={'entries':0,'regular_files':0,'regular_bytes':0}
    binary=work/'backend-uniroute' if role=='backend' else None
    def consume(inp):
        seen=set()
        with tarfile.open(fileobj=inp,mode='r|') as tf:
            for member in tf:
                time_guard(); path=PurePosixPath(member.name)
                require(not path.is_absolute() and '..' not in path.parts and '\x00' not in member.name,'unsafe rootfs member')
                name=str(path); require(name not in seen,'duplicate rootfs path'); seen.add(name)
                stats['entries']+=1
                if not member.isfile(): continue  # Never follow links or mounted volumes.
                stats['regular_files']+=1; stats['regular_bytes']+=member.size
                notice=bool(re.search(r'(^|/)([^/]*(?:licen[cs]e|copyright|notice)[^/]*)(/|$)',name,re.I))
                db=name in ['lib/apk/db/installed','var/lib/dpkg/status']
                if db: require(member.size<=16*1024*1024,'package database too large')
                chunks=[]; h=hashlib.sha256(); bf=None
                if binary is not None and name=='app/uniroute': bf=binary.open('xb'); os.chmod(binary,0o644)
                try:
                    with tf.extractfile(member) as stream:
                        while True:
                            data=stream.read(65536)
                            if not data: break
                            h.update(data)
                            if db: chunks.append(data)
                            if bf: bf.write(data)
                finally:
                    if bf: bf.close()
                if notice: notices.append({'path':name,'size':member.size,'sha256':h.hexdigest()})
                if db: databases[name]=(b''.join(chunks),h.hexdigest())
    stream_command(role+' stopped rootfs export',['docker','--config',str(work/'docker-config'),
                   '--host','unix:///var/run/docker.sock','export',cid],consume)
    packages=apk_packages(databases['lib/apk/db/installed'][0]) if 'lib/apk/db/installed' in databases else []
    receipt['images'][role]['filesystem_inventory']={**stats,
        'notices':sorted(notices,key=lambda row:row['path']),
        'apk':[{'name':row['name'],'version':row['version'],'declared_license':row['license'],'origin':row['origin']} for row in packages],
        'databases':[{'path':name,'size':len(data),'sha256':sha} for name,(data,sha) in sorted(databases.items())],
        'dpkg_unparsed':'var/lib/dpkg/status' in databases}
    snapshot(); return binary

def cleanup():
    primary_failed=receipt['status']!='built_candidate_with_unresolved_obligations'
    for cid in reversed(owned):
        try:
            meta=json.loads(docker('cleanup ownership','container','inspect',cid,cleanup=True))[0]
            require(meta['Id']==cid and meta['Config']['Labels'].get('ur.batchb.run')==run_id,'cleanup ownership mismatch')
            if meta['State']['Running']: docker('stop owned tool process','stop','--time','2',cid,cleanup=True)
            docker('remove owned container and anonymous volumes','rm','-v',cid,cleanup=True)
            receipt['cleanup'].append({'id':cid,'removed_with_anonymous_volumes':True})
        except Exception as e:
            diagnostic('cleanup',e,stage='cleanup')
            receipt['cleanup'].append({'id':cid,'removed_with_anonymous_volumes':False,'error_class':type(e).__name__})
    if not all(row['removed_with_anonymous_volumes'] for row in receipt['cleanup']):
        receipt['status']='stopped_partial_candidate'
        if not primary_failed: receipt.update(stage='cleanup',error_class='IncompleteCandidate')
    snapshot()

def collect_images(source,back,front,encoded,go):
    args=['RUNTIME_VERSION='+FIXED['source']['version'],'RUNTIME_COMMIT='+FIXED['source']['commit'],
          'RUNTIME_TREE='+FIXED['source']['tree'],'UNIROUTER_RELEASE_TRUST_MODE=release',
          'UNIROUTER_UPGRADE_TRUST_BUNDLE_B64='+encoded,'GOPROXY=https://proxy.golang.org,direct']
    command=['build','--platform','linux/amd64','--file',str(back),'--tag','unirouter-batch-b-backend:'+run_id,
             '--iidfile',str(work/'backend.id')]
    for arg in args: command += ['--build-arg',arg]
    receipt['stage']='backend_build'
    docker('one release backend build',*command,str(source))
    bid=(work/'backend.id').read_text().strip(); require(bool(re.fullmatch('sha256:[0-9a-f]{64}',bid)),'backend image ID absent')
    inspect_image('backend',bid)
    require(receipt['images']['backend']['rootfs_diff_ids'][:len(FIXED['images']['backend-runtime']['rootfs_diff_ids'])]==FIXED['images']['backend-runtime']['rootfs_diff_ids'],'backend base rootfs mismatch')
    receipt['stage']='frontend_build'
    docker('one production frontend build','build','--platform','linux/amd64','--file',str(front),
           '--build-arg','VITE_DEPLOY_ENV=production','--build-arg','VITE_APP_VERSION='+FIXED['source']['version'],
           '--tag','unirouter-batch-b-frontend:'+run_id,'--iidfile',str(work/'frontend.id'),str(source/'frontend'))
    fid=(work/'frontend.id').read_text().strip(); require(bool(re.fullmatch('sha256:[0-9a-f]{64}',fid)),'frontend image ID absent')
    inspect_image('frontend',fid)
    require(receipt['images']['frontend']['rootfs_diff_ids'][:len(FIXED['images']['frontend-runtime']['rootfs_diff_ids'])]==FIXED['images']['frontend-runtime']['rootfs_diff_ids'],'frontend base rootfs mismatch')
    receipt['stage']='application_inventory'
    binary=inventory('backend',bid)
    require(binary is not None and binary.is_file(),'backend binary missing from actual image')
    gi=tool('go-buildinfo',go,'mkdir -p /tmp/telemetry; printf "off\\n" > /tmp/telemetry/mode; go version -m /inspect/uniroute',binary)
    for value in ['go1.25.12','CGO_ENABLED=0','GOOS=linux','GOARCH=amd64']:
        require(value in gi,'backend build metadata mismatch')
    for value in ['main.buildVersion='+FIXED['source']['version'],'main.buildCommit='+FIXED['source']['commit'],
                  'main.buildTree='+FIXED['source']['tree'],'github.com/uniroute/unirouter/internal/releasetrust.buildMode=release',
                  'github.com/uniroute/unirouter/internal/releasetrust.embeddedUpgradeKeyRingBase64='+encoded]: require(value in gi,'backend release argument missing in build-info')
    receipt['backend_binary']={'sha256':hash_file(binary),'bytes':binary.stat().st_size,'actual_go_build_info_checked':True}
    modules=[line.strip().split('\t') for line in gi.splitlines() if line.strip().split('\t')[0] in ['mod','dep','=>']]
    receipt['go_modules']=modules
    inventory('frontend',fid)
    for role in ['postgres','redis','prometheus','grafana']:
        stage='companion_inventory'; receipt['stage']=stage
        image=pull(role); inventory(role,image)

def main():
    global work,delivered,pending,docker_root,child_env,deadline,cleanup_deadline,run_id
    receipt['stage']='preflight'
    require(sys.version_info>=(3,10),'Python3.10+ required')
    require(os.environ.get('GITHUB_REPOSITORY')==FIXED['source']['repository'],'wrong workflow repository')
    require(os.environ.get('GITHUB_EVENT_NAME')=='workflow_dispatch' and os.environ.get('GITHUB_REF')=='refs/heads/main','manual approved main only')
    require(os.environ.get('RUNNER_ENVIRONMENT')=='github-hosted' and os.environ.get('RUNNER_OS')=='Linux'
            and os.environ.get('RUNNER_ARCH')=='X64' and platform.machine()=='x86_64','native hosted Linux amd64 required')
    require(os.environ.get('GITHUB_RUN_ATTEMPT')=='1','rerun not authorized')
    expected=os.environ.get('EXPECTED_WORKFLOW_COMMIT','')
    require(re.fullmatch('[0-9a-f]{40}',expected) and expected==os.environ.get('GITHUB_SHA'),'workflow commit drift')
    require(not any(os.environ.get(k) for k in ['DOCKER_HOST','DOCKER_CONTEXT','DOCKER_TLS_VERIFY','DOCKER_CERT_PATH','BUILDKIT_HOST','BUILDX_BUILDER']),'inherited remote/auth context forbidden')
    run_id=os.environ.get('GITHUB_RUN_ID',''); require(bool(re.fullmatch('[0-9]{1,24}',run_id)),'invalid run identity')
    job_start=os.environ.get('UR_IMAGE_JOB_MONOTONIC','')
    require(bool(re.fullmatch(r'[0-9]+(?:\.[0-9]+)?',job_start)),'first workflow-step clock missing')
    job_start=float(job_start); require(0<job_start<=time.monotonic(),'invalid job clock')
    deadline=job_start+HELPER_SECONDS; cleanup_deadline=job_start+40*60
    time_guard()
    base=Path(os.environ['RUNNER_TEMP']).resolve(); require(base.is_dir(),'runner temp missing')
    work=base/('ur-image-candidate-'+run_id+'-attempt1'); require(not os.path.lexists(work),'preserve preexisting candidate path')
    work.mkdir(mode=0o700); delivered=work/'deliverable'; pending=work/'output-staging'; delivered.mkdir(mode=0o700); pending.mkdir(mode=0o700)
    (work/'docker-config').mkdir(mode=0o700); (work/'docker-config/config.json').write_text('{"auths":{}}\n')
    child_env={'PATH':os.environ['PATH'],'LANG':'C.UTF-8','LC_ALL':'C.UTF-8','TZ':'UTC',
               'DOCKER_CONFIG':str(work/'docker-config'),'DOCKER_BUILDKIT':'1','TMPDIR':str(work),
               'GIT_CONFIG_NOSYSTEM':'1','GIT_CONFIG_GLOBAL':'/dev/null','GIT_ATTR_NOSYSTEM':'1'}
    receipt.update(status='running',started_utc=utc(),workflow_commit=expected,run_id=run_id,helper_sha256=hash_file(Path(__file__)),
                   limits={'helper_seconds':HELPER_SECONDS,'job_minutes':45,'report_bytes':MAX_REPORT,'initial_free_bytes_each_filesystem':INITIAL_FREE,
                           'phase_or_output_write_minimum_free_bytes':MIN_FREE,'command_log_bytes_each':MAX_LOG_BYTES,
                           'disk_monitoring':'Before commands and at uploaded-output writes, not continuous during Docker work.',
                           'declared_oci_bytes':FIXED['declared_unique_compressed_layer_bytes'],'dependency_download_bytes':'unmeasured'},
                   partial_retention='Only a validated terminal facts report is uploaded; all images, archives and logs expire with the runner. Hard cancellation or report rejection can leave no artifact.')
    receipt['stage']='preflight'
    try:
        require(Path(os.environ['GITHUB_WORKSPACE']).resolve()==CODE,'helper checkout location drift')
        head=argv_run('workflow checkout identity',['git','-C',str(CODE),'rev-parse','HEAD']); require(head==expected,'helper checkout drift')
        event=json.loads(Path(os.environ['GITHUB_EVENT_PATH']).read_text()); repo=event['repository']
        require(repo['full_name']==FIXED['source']['repository'],'event repository drift')
        receipt['repository']={'id':repo['id'],'full_name':repo['full_name'],'private':repo['private']}
        receipt['artifact_access']='Repository visibility/access applies; artifact is not asserted private. Exact sharing/source obligations need approval before this job.'
        encoded=os.environ.get('UR_UPGRADE_RING_B64',''); require(bool(re.fullmatch('[A-Za-z0-9_-]{111}',encoded)),'invalid public input encoding')
        raw=base64.urlsafe_b64decode(encoded+'='); ring=FIXED['approved_public_ring']
        require(len(raw)==ring['bytes'] and digest(raw)==ring['sha256'] and base64.urlsafe_b64encode(raw).decode().rstrip('=')==encoded,'public ring byte identity mismatch')
        keys=json.loads(raw); require(list(keys)==[ring['key_id']],'public ring key scope mismatch')
        key=base64.b64decode(keys[ring['key_id']],validate=True)
        require(len(key)==32 and digest(key)==ring['decoded_key_sha256'],'public fingerprint mismatch')
        info=json.loads(docker('Docker environment','info','--format',
                        '{"OSType":{{json .OSType}},"Architecture":{{json .Architecture}},"SecurityOptions":{{json .SecurityOptions}},"DockerRootDir":{{json .DockerRootDir}},"ServerVersion":{{json .ServerVersion}}}'))
        require(info['OSType']=='linux' and info['Architecture'] in ['x86_64','amd64'] and
                not any('rootless' in x for x in info.get('SecurityOptions',[])),'local rootful native Docker required')
        docker_root=Path(info['DockerRootDir']); require(docker_root.is_absolute() and docker_root.is_dir(),'DockerRootDir inaccessible')
        receipt['environment']={'python':platform.python_version(),'machine':platform.machine(),'docker_root':str(docker_root),
                                 'docker_server_version':info['ServerVersion'],'task_free':shutil.disk_usage(work).free,'docker_free':shutil.disk_usage(docker_root).free,
                                 'docker_versions':docker('Docker versions','version','--format','{{json .}}'),
                                 'compose_version':docker('Compose version','compose','version','--short')}
        disk_guard(initial=True)
        receipt['stage']='source'
        source=source_export(); back,front=recipes(source)
        receipt['stage']='tools'
        go=pull('backend-builder'); node=pull('frontend-builder')
        go_version=tool('go-version',go,'mkdir -p /tmp/telemetry; printf "off\\n" > /tmp/telemetry/mode; go version')
        receipt['actual_build_tools']={'go':go_version}
        require(go_version=='go version go1.25.12 linux/amd64','actual Go version mismatch')
        node_versions=tool('node-version',node,'node --version && npm --version').splitlines()
        if len(node_versions)==2: receipt['actual_build_tools'].update(node=node_versions[0],npm=node_versions[1])
        require(node_versions==['v24.19.0','11.17.0'],'actual Node/npm mismatch; do not install or relax')
        receipt['actual_build_tools']={'go':go_version,'node':node_versions[0],'npm':node_versions[1]}; snapshot()
        receipt['stage']='base_images'
        for role in ['backend-runtime','frontend-runtime']: pull(role)
        collect_images(source,back,front,encoded,go)
        receipt['status']='built_candidate_with_unresolved_obligations'
    except Exception as e:
        diagnostic('main',e)
        receipt.update(status='stopped_partial_candidate',error_class=type(e).__name__)
    finally:
        primary_failed=receipt['status']!='built_candidate_with_unresolved_obligations'
        try:
            cleanup()
        except Exception as e:
            diagnostic('cleanup',e,stage='cleanup')
            receipt['status']='stopped_partial_candidate'
            if not primary_failed: receipt.update(stage='cleanup',error_class=type(e).__name__)
        try:
            receipt['ended_utc']=utc()
            if not all(c['removed_with_anonymous_volumes'] for c in receipt['cleanup']):
                receipt.update(status='stopped_partial_candidate',error='Owned cleanup incomplete; runner lifecycle is the final resource boundary.')
            receipt['elapsed_from_first_workflow_step_seconds']=round(time.monotonic()-job_start,3)
            snapshot()
            write_facts_report()
            print(json.dumps({'report':FACTS_FILENAME,'status':project_facts()['status'],'run_id':run_id}),flush=True)
        except Exception as e:
            diagnostic('report',e)
            raise
    return 0 if receipt['status']=='built_candidate_with_unresolved_obligations' and all(c['removed_with_anonymous_volumes'] for c in receipt['cleanup']) else 1


STAGES={'preflight','source','tools','base_images','backend_build','frontend_build','application_inventory','companion_inventory','cleanup'}
GAPS=['image_license_review_pending','frontend_membership_incomplete','prometheus_grafana_sources_unbound',
      'application_registry_digests_unbound','signed_release_absent','public_install_unverified',
      'rootfs_links_and_volumes_excluded','notice_bodies_not_retained']
IMAGE_ROLES=set(FIXED['images'])|{'backend','frontend'}
CREATE_ROLES={'go-version','node-version','go-buildinfo'}|{r+'-inventory' for r in ['backend','frontend','postgres','redis','prometheus','grafana']}
COMMAND_PHASES={'workflow checkout identity','Docker environment','Docker versions','Compose version','source commit','source tree',
 'export tracked source','one release backend build','one production frontend build','cleanup ownership',
 'stop owned tool process','remove owned container and anonymous volumes'}
COMMAND_PHASES|={r+' '+suffix for r in IMAGE_ROLES for suffix in ['pull','image inspect']}
COMMAND_PHASES|={r+' '+suffix for r in CREATE_ROLES for suffix in ['create','container identity','version/readback']}
COMMAND_PHASES|={r+' stopped rootfs export' for r in ['backend','frontend','postgres','redis','prometheus','grafana']}

def exact(value,keys):
    require(type(value) is dict and set(value)==set(keys),'REPORT_SCHEMA_REJECTED')

def text_field(value,pattern,limit=512,nullable=False):
    require((nullable and value is None) or (type(value) is str and len(value)<=limit and re.fullmatch(pattern,value) is not None),'REPORT_VALUE_REJECTED')

def integer(value,nullable=False):
    require((nullable and value is None) or (type(value) is int and 0<=value<=2**63-1),'REPORT_VALUE_REJECTED')

def flag(value): require(type(value) is bool,'REPORT_VALUE_REJECTED')

def hex_digest(value,nullable=False): text_field(value,r'[0-9a-f]{64}',64,nullable)

def rows(value): require(type(value) is list and len(value)<=10000,'REPORT_VALUE_REJECTED')

def project_facts():
    images={}
    for role,row in receipt['images'].items():
        images[role]={'id':row['id'],'platform':row['platform'],'rootfs_diff_ids':row['rootfs_diff_ids'],
            'matched_pull_reference':row['exact_installer_pull_candidate'],
            'registry_digest_unbound':row['registry_digest_unbound'],'inventory':row.get('filesystem_inventory')}
    tools=receipt.get('actual_build_tools',{}); environment=receipt.get('environment',{})
    commands=[{'phase':r['phase'],'status':r['status'],'exit_code':r.get('exit_code'),
               'log_bytes':r.get('log_bytes'),'log_sha256':r.get('log_sha256'),
               'receiver_complete':r.get('log_receiver_complete'),'process_exit_unconfirmed':r.get('process_exit_unconfirmed',False)} for r in receipt['commands']]
    modules=[]
    for row in receipt.get('go_modules',[]):
        require(len(row) in (3,4),'REPORT_MODULE_REJECTED')
        modules.append({'kind':row[0],'path':row[1],'version':row[2],'sum':row[3] if len(row)==4 else None})
    builds=all(any(r['phase']==p and r['status']=='success' for r in commands) for p in ['one release backend build','one production frontend build'])
    observed=lambda role: role in images and images[role]['inventory'] is not None
    cleanup=[{'id':r['id'],'removed_with_anonymous_volumes':r['removed_with_anonymous_volumes']} for r in receipt['cleanup']]
    complete=receipt['status']=='built_candidate_with_unresolved_obligations' and all(r['removed_with_anonymous_volumes'] for r in cleanup)
    return {'schema_version':'unirouter.image-facts.v1','status':'facts_collected' if complete else 'stopped_partial_candidate',
      'expected_source':FIXED['source'],'observed_source':receipt.get('source_export'),
      'expected_inputs':{r:{'pull_reference':v['pull_reference'],'config_id':v['config_id']} for r,v in FIXED['images'].items()},
      'public_trust':{'key_id':FIXED['approved_public_ring']['key_id'],'ring_sha256':FIXED['approved_public_ring']['sha256'],
                      'public_key_sha256':FIXED['approved_public_ring']['decoded_key_sha256']},
      'run':{'workflow_commit':receipt['workflow_commit'],'run_id':run_id,'attempt':1,'helper_sha256':receipt['helper_sha256'],
             'started_utc':receipt['started_utc'],'ended_utc':receipt['ended_utc'],'elapsed_seconds':receipt['elapsed_from_first_workflow_step_seconds']},
      'tools':{'go':tools.get('go'),'node':tools.get('node'),'npm':tools.get('npm'),'python':environment.get('python'),
               'docker_server':environment.get('docker_server_version'),'compose':environment.get('compose_version')},
      'images':images,'go_modules':modules,'backend_binary':receipt.get('backend_binary'),
      'commands':commands,'cleanup':cleanup,
      'failure':None if complete else {'stage':receipt.get('stage','preflight'),'class':receipt.get('error_class','IncompleteCandidate')},
      'completion':{'application_builds_completed':builds,'application_inventory_completed':all(observed(r) for r in ['backend','frontend']),
                    'companion_inventory_completed':all(observed(r) for r in ['postgres','redis','prometheus','grafana']),
                    'distribution_ready':False,'public_install_verified':False,'complete_sbom_or_license_clearance':False},
      'gaps':GAPS}

def validate_facts(value):
    exact(value,['schema_version','status','expected_source','observed_source','expected_inputs','public_trust','run','tools','images','go_modules',
                 'backend_binary','commands','cleanup','failure','completion','gaps'])
    require(value['schema_version']=='unirouter.image-facts.v1' and value['status'] in ['facts_collected','stopped_partial_candidate'],'REPORT_SCHEMA_REJECTED')
    require(value['expected_source']==FIXED['source'] and type(value['expected_source']) is dict,'REPORT_SOURCE_REJECTED')
    for k in ['tracked_files','tracked_regular_bytes']:integer(value['expected_source'][k])
    observed=value['observed_source']
    if observed is not None:
        exact(observed,['commit','tree','files','regular_bytes','archive_sha256'])
        require(observed['commit']==FIXED['source']['commit'] and observed['tree']==FIXED['source']['tree'],'REPORT_SOURCE_REJECTED')
        integer(observed['files']);integer(observed['regular_bytes']);hex_digest(observed['archive_sha256'])
        require(observed['files']==FIXED['source']['tracked_files'] and observed['regular_bytes']==FIXED['source']['tracked_regular_bytes'],'REPORT_SOURCE_REJECTED')
    require(value['expected_inputs']=={r:{'pull_reference':v['pull_reference'],'config_id':v['config_id']} for r,v in FIXED['images'].items()},'REPORT_INPUT_REJECTED')
    require(value['public_trust']=={'key_id':FIXED['approved_public_ring']['key_id'],'ring_sha256':FIXED['approved_public_ring']['sha256'],
             'public_key_sha256':FIXED['approved_public_ring']['decoded_key_sha256']},'REPORT_TRUST_REJECTED')
    run=value['run'];exact(run,['workflow_commit','run_id','attempt','helper_sha256','started_utc','ended_utc','elapsed_seconds'])
    text_field(run['workflow_commit'],r'[0-9a-f]{40}',40);text_field(run['run_id'],r'[0-9]{1,24}',24);require(type(run['attempt']) is int and run['attempt']==1,'REPORT_VALUE_REJECTED')
    hex_digest(run['helper_sha256'])
    for k in ['started_utc','ended_utc']: text_field(run[k],r'[0-9T:.+Z-]{20,40}',40)
    require(type(run['elapsed_seconds']) in (int,float) and 0<=run['elapsed_seconds']<=45*60,'REPORT_VALUE_REJECTED')
    tools=value['tools'];exact(tools,['go','node','npm','python','docker_server','compose'])
    for k in tools:
        pattern=r'go version go[0-9.]+ linux/amd64' if k=='go' else r'v?[0-9]+\.[0-9]+\.[0-9]+(?:[-+][A-Za-z0-9._-]+)?'
        text_field(tools[k],pattern,80,True)
    require(type(value['images']) is dict and set(value['images'])<=IMAGE_ROLES,'REPORT_IMAGE_REJECTED')
    for role,image in value['images'].items():
        exact(image,['id','platform','rootfs_diff_ids','matched_pull_reference','registry_digest_unbound','inventory'])
        text_field(image['id'],r'sha256:[0-9a-f]{64}',71)
        require(image['platform']=={'os':'linux','arch':'amd64'},'REPORT_IMAGE_REJECTED')
        rows(image['rootfs_diff_ids'])
        for layer in image['rootfs_diff_ids']: text_field(layer,r'sha256:[0-9a-f]{64}',71)
        flag(image['registry_digest_unbound'])
        if role in FIXED['images']:
            ref=image['matched_pull_reference'];text_field(ref,r'[a-z0-9./_-]+@sha256:[0-9a-f]{64}',256)
            repository,digest_part=ref.split('@');parts=repository.split('/')
            if parts[0] in ['docker.io','index.docker.io','registry-1.docker.io']:parts=parts[1:]
            if len(parts)==1:parts=['library',parts[0]]
            require('docker.io/'+'/'.join(parts)+'@'+digest_part==FIXED['images'][role]['pull_reference'] and image['registry_digest_unbound'] is False,'REPORT_IMAGE_REJECTED')
        else: require(image['matched_pull_reference'] is None and image['registry_digest_unbound'] is True,'REPORT_IMAGE_REJECTED')
        inv=image['inventory']
        if inv is None: continue
        exact(inv,['entries','regular_files','regular_bytes','notices','apk','databases','dpkg_unparsed'])
        for k in ['entries','regular_files','regular_bytes']:integer(inv[k])
        flag(inv['dpkg_unparsed'])
        rows(inv['notices']);rows(inv['apk']);rows(inv['databases'])
        for row in inv['notices']+inv['databases']:
            exact(row,['path','size','sha256']);text_field(row['path'],r'[A-Za-z0-9_.,+@/() -]+',512)
            require(not PurePosixPath(row['path']).is_absolute() and '..' not in PurePosixPath(row['path']).parts,'REPORT_PATH_REJECTED')
            integer(row['size']);hex_digest(row['sha256'])
        for row in inv['databases']:require(row['path'] in ['lib/apk/db/installed','var/lib/dpkg/status'],'REPORT_PATH_REJECTED')
        for row_index,row in enumerate(inv['apk']):
            exact(row,['name','version','declared_license','origin'])
            for k in row:
                # Preserve an observed empty declaration; missing remains None.
                if k=='declared_license' and type(row[k]) is str and row[k]=='': continue
                try: text_field(row[k],r'[A-Za-z0-9_.+():/ -]+',256,k in ['declared_license','origin'])
                except Stop:
                    try: apk_field_witness(role,image,inv,row_index,k,row[k])
                    except Exception: pass  # Preserve the original rejection if diagnostics fail.
                    raise
    rows(value['go_modules'])
    for row in value['go_modules']:
        exact(row,['kind','path','version','sum']);require(row['kind'] in ['mod','dep','=>'],'REPORT_MODULE_REJECTED')
        text_field(row['path'],r'[A-Za-z0-9][A-Za-z0-9._~+/-]*',256)
        text_field(row['version'],r'(?:[A-Za-z0-9][A-Za-z0-9._+~-]*|\(devel\))',128)
        text_field(row['sum'],r'h1:[A-Za-z0-9+/]{43}=',47,True)
    if value['backend_binary'] is not None:
        exact(value['backend_binary'],['sha256','bytes','actual_go_build_info_checked']);hex_digest(value['backend_binary']['sha256']);integer(value['backend_binary']['bytes'])
        require(value['backend_binary']['actual_go_build_info_checked'] is True,'REPORT_VALUE_REJECTED')
    rows(value['commands'])
    for row in value['commands']:
        exact(row,['phase','status','exit_code','log_bytes','log_sha256','receiver_complete','process_exit_unconfirmed'])
        require(row['phase'] in COMMAND_PHASES and row['status'] in ['success','failed','timeout'],'REPORT_COMMAND_REJECTED')
        require(row['exit_code'] is None or (type(row['exit_code']) is int and -255<=row['exit_code']<=255),'REPORT_VALUE_REJECTED')
        integer(row['log_bytes'],True);hex_digest(row['log_sha256'],True)
        require(row['receiver_complete'] is None or type(row['receiver_complete']) is bool,'REPORT_VALUE_REJECTED');flag(row['process_exit_unconfirmed'])
        if row['status']=='success': require(row['exit_code']==0 and row['receiver_complete'] is True and row['process_exit_unconfirmed'] is False,'REPORT_COMMAND_REJECTED')
    rows(value['cleanup'])
    for row in value['cleanup']:
        exact(row,['id','removed_with_anonymous_volumes']);hex_digest(row['id']);flag(row['removed_with_anonymous_volumes'])
    if value['failure'] is not None:
        exact(value['failure'],['stage','class']);require(value['failure']['stage'] in STAGES,'REPORT_VALUE_REJECTED')
        require(value['failure']['class'] in ['Stop','CommandTimeout','TimeoutExpired','OSError','ValueError','KeyError','TypeError','JSONDecodeError','FileNotFoundError','PermissionError','IncompleteCandidate','OtherError'],'REPORT_VALUE_REJECTED')
    exact(value['completion'],['application_builds_completed','application_inventory_completed','companion_inventory_completed','distribution_ready','public_install_verified','complete_sbom_or_license_clearance'])
    for k,v in value['completion'].items():flag(v)
    for k in ['distribution_ready','public_install_verified','complete_sbom_or_license_clearance']:require(value['completion'][k] is False,'REPORT_VALUE_REJECTED')
    require(value['gaps']==GAPS,'REPORT_GAPS_REJECTED')
    completion=value['completion']
    builds=all(any(r['phase']==p and r['status']=='success' for r in value['commands']) for p in ['one release backend build','one production frontend build'])
    observed=lambda role: role in value['images'] and value['images'][role]['inventory'] is not None
    require(completion['application_builds_completed']==builds and completion['application_inventory_completed']==all(observed(r) for r in ['backend','frontend']) and completion['companion_inventory_completed']==all(observed(r) for r in ['postgres','redis','prometheus','grafana']),'REPORT_COMPLETION_REJECTED')
    if value['status']=='facts_collected':
        require(value['observed_source'] is not None and all(r['status']=='success' for r in value['commands']) and value['failure'] is None and all(value['completion'][k] for k in ['application_builds_completed','application_inventory_completed','companion_inventory_completed']) and all(r['removed_with_anonymous_volumes'] for r in value['cleanup']),'REPORT_COMPLETION_REJECTED')
    else: require(value['failure'] is not None,'REPORT_COMPLETION_REJECTED')

def facts_bytes(value):
    validate_facts(value)
    raw=(json.dumps(value,sort_keys=True,separators=(',',':'),allow_nan=False)+'\n').encode()
    require(len(raw)<=MAX_REPORT,'REPORT_SIZE_REJECTED');return raw

def write_facts_report():
    value=project_facts()
    if value['failure'] is not None and value['failure']['class'] not in ['Stop','CommandTimeout','TimeoutExpired','OSError','ValueError','KeyError','TypeError','JSONDecodeError','FileNotFoundError','PermissionError','IncompleteCandidate']:
        value['failure']['class']='OtherError'
    raw=facts_bytes(value);path=delivered/FACTS_FILENAME;partial=work/'facts-report.partial'
    require(not path.exists() and not partial.exists(),'REPORT_PATH_REJECTED')
    with partial.open('xb') as stream:stream.write(raw)
    os.chmod(partial,0o600);partial.rename(path)

def check_facts_file(path):
    require(path.name==FACTS_FILENAME and stat.S_ISREG(path.lstat().st_mode) and not path.is_symlink(),'REPORT_PATH_REJECTED')
    require(path.stat().st_size<=MAX_REPORT,'REPORT_SIZE_REJECTED')
    def unique(pairs):
        result={}
        for key,value in pairs:
            require(key not in result,'REPORT_SCHEMA_REJECTED');result[key]=value
        return result
    raw=path.read_bytes();require(len(raw)<=MAX_REPORT,'REPORT_SIZE_REJECTED')
    value=json.loads(raw,object_pairs_hook=unique);facts_bytes(value)

if __name__=='__main__':
    try:
        if len(sys.argv)==3 and sys.argv[1]=='--validate-report':
            check_facts_file(Path(sys.argv[2]));print('UR_FACTS_REPORT_VALID');sys.exit(0)
        require(len(sys.argv)==1,'INVALID_ARGUMENTS')
        sys.exit(main())
    except Exception as e:
        diagnostic('validate_report' if len(sys.argv)==3 and sys.argv[1]=='--validate-report' else 'module',e)
        try: print('UR_IMAGE_FACTS_REFUSED',file=sys.stderr,flush=True)
        except Exception: pass
        sys.exit(1)
