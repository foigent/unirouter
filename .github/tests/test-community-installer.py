#!/usr/bin/env python3
"""Offline checks of the actual native fixture and shipped bootstrap contract."""
import ast
from contextlib import redirect_stderr
import hashlib
import importlib.util
import ipaddress
import io
import json
from pathlib import Path
import shutil
import subprocess
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import MagicMock, patch

ROOT = Path(__file__).resolve().parents[2]

def load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module

fixture = load('installer_fixture', ROOT / '.github/tests/community-installer.py')
bootstrap = load('actual_bootstrap', ROOT / 'deploy/community/bootstrap.py')

class InstallerFixtureContract(unittest.TestCase):
    def test_independent_cases_use_one_explicit_port_for_commands_and_HTTP(self):
        self.assertEqual(fixture.FIXTURE_PORTS, {'fresh':13000,'single':13001,'edge':13002})
        work=object.__new__(fixture.Acceptance)
        for name in ('repo','release','verifier','ring','bootstrap_file'):
            setattr(work,name,Path('/fixture')/name)
        work.bootstrap_sha='a'*64
        self.assertEqual(work.loopback_port,13000)
        for layout,port in fixture.FIXTURE_PORTS.items():
            with self.subTest(layout=layout):
                work.loopback_port=port
                for command in (work.command('install',Path('/fixture/instance'),'fixture'),
                                work.bootstrap_command('install',Path('/fixture/instance'),'fixture')):
                    self.assertEqual(command[command.index('--port')+1],str(port))
                self.assertEqual(work.loopback_url,'http://127.0.0.1:'+str(port))

    def test_actual_retained_installer_rejects_completed_case_network_until_released(self):
        with tempfile.TemporaryDirectory() as tmp:
            root=Path(tmp).resolve();path=root/'retained-install.py'
            path.write_bytes(subprocess.check_output(['git','-C',str(ROOT),'show',fixture.RETAINED_COMMIT+':deploy/customer/install.py']))
            owner=load('retained_network_guard',path)
            sample=subprocess.check_output(['git','-C',str(ROOT),'show',fixture.RETAINED_COMMIT+':deploy/customer/.env.example']).decode()
            subnet=next(line.split('=',1)[1] for line in sample.splitlines() if line.startswith('UNIROUTER_INTERNAL_NETWORK_CIDR='))
            release=SimpleNamespace(directory=root/'release',package=root/'release.tar.gz',subnet=ipaddress.ip_network(subnet))
            previous_network=True;calls=[]
            def run(args,**kwargs):
                calls.append(args)
                if args[-2:]==['compose','version']:return 'Docker Compose native-boundary fixture'
                if args[-3:]==['info','--format','{{json .}}']:return '{"OSType":"linux","ID":"original-synthetic-daemon","SecurityOptions":[]}'
                if '--filter' in args or args[-1] in ('{{.Names}}','{{.Name}}'):return ''
                if args[-4:]==['network','ls','-q','--no-trunc']:return 'earlier-case-network\n' if previous_network else ''
                if args[-3:]==['network','inspect','earlier-case-network']:
                    return json.dumps([{'IPAM':{'Config':[{'Subnet':subnet}]}}])
                self.fail('unexpected native read boundary')
            operation=owner.Installer(release,root/'next-case','next-case',13000,Path('/var/run/docker.sock'),run)
            with patch.object(owner.platform,'system',return_value='Linux'),patch.object(owner.os,'geteuid',return_value=0), \
                 patch.object(owner.socket,'socket',return_value=MagicMock()):
                with self.assertRaisesRegex(owner.InstallError,'signed release subnet overlaps'):
                    operation.check()
                previous_network=False
                self.assertEqual(operation.check()['ID'],'original-synthetic-daemon')
            self.assertTrue(all(not any(command in args for command in ('up','stop','down','rm')) for args in calls))

    def test_completed_case_cleanup_uses_exact_original_chains_and_preserves_volumes(self):
        for layout in ('fresh','single','edge'):
            with self.subTest(layout=layout):
                work=object.__new__(fixture.Acceptance);work.docker='/fixture/docker';calls=[]
                work.run=lambda label,args:calls.append((label,[str(arg) for arg in args]))
                directory=Path('/fixture/'+layout);project='ur-'+layout
                edge=directory/'edge' if layout=='edge' else None
                base=[str(arg) for arg in work.compose(directory,project)]
                work.release_completed_case(directory,project,edge,upgraded=layout!='fresh')
                expected=[]
                if edge:
                    expected.append([work.docker,'compose','--project-name',project+'-edge','--project-directory',str(edge),
                        '--env-file','/dev/null','-f',str(edge/'compose.edge.json'),'down'])
                    base+=['-f',str(edge/'runtime-overrides.json')]
                if layout!='fresh':base+=['-f',str(directory/'runtime/upgrade-image.json')]
                expected.append(base+['down'])
                self.assertEqual([args for _,args in calls],expected)
                for _,args in calls:
                    self.assertTrue(set(args).isdisjoint({'-v','--volumes','--rmi','--remove-orphans'}))

    def test_execute_releases_only_verified_cases_in_fresh_single_edge_order(self):
        for failure in (None,'fresh','single'):
            with self.subTest(failure=failure):
                work=object.__new__(fixture.Acceptance);work.docker='/fixture/docker'
                work.instances=Path('/fixture/instances');work.suffix='synthetic';work.schema=fixture.CURRENT_SCHEMA;work.result={}
                events=[];downs=[]
                def verify(label):
                    events.append(label)
                    if failure==label:raise fixture.Failed('preserve failed case')
                for name in ('preflight','prepare','negative_checks','interrupt_after_plan','installed_checks'):
                    setattr(work,name,lambda *args:None)
                work.native_first_use_checks=lambda *args:verify('fresh')
                def run(label,args):
                    args=[str(arg) for arg in args];downs.append(args);events.append('down:'+args[args.index('--project-name')+1])
                work.run=run
                def retained():
                    events.append('prepare:retained')
                    self.assertEqual(len(downs),1,'fresh network released before retained installation')
                    return SimpleNamespace()
                work.retained_release=retained
                def qualify(old,layout):
                    verify(layout)
                    directory=Path('/fixture/retained')/layout
                    work.release_completed_case(directory,'ur-'+layout,directory/'edge' if layout=='edge' else None,upgraded=True)
                work.qualify_upgrade=qualify
                if failure:
                    with self.assertRaisesRegex(fixture.Failed,'preserve failed case'):work.execute()
                    self.assertEqual(len(downs),0 if failure=='fresh' else 1)
                else:
                    work.execute()
                    self.assertEqual(events,['fresh','down:ur-native-synthetic','prepare:retained','single','down:ur-single',
                        'edge','down:ur-edge-edge','down:ur-edge'])
                    self.assertEqual(work.result['status'],'passed')
                    self.assertIn('/fixture/retained/single/runtime/upgrade-image.json',downs[1])
                    self.assertIn('/fixture/retained/edge/edge/runtime-overrides.json',downs[3])
                    self.assertIn('/fixture/retained/edge/runtime/upgrade-image.json',downs[3])

    def test_retained_install_failure_reports_its_actual_owner_and_only_authored_error(self):
        for layout,authored in (('single',None),('edge','Installation stopped: The signed release subnet overlaps an existing Docker network')):
            with self.subTest(layout=layout,authored=authored):
                work=object.__new__(fixture.Acceptance);work.suffix='synthetic';work.result={'installer_stop':'stale earlier negative'}
                old=SimpleNamespace(instances=Path('/fixture/retained'),result={},phase='before')
                old.command=lambda *args:['not-executed']
                def fail(label,args):
                    old.phase=label
                    if authored:old.result['installer_stop']=authored
                    raise fixture.Failed(label+' failed')
                old.run=fail
                with self.assertRaisesRegex(fixture.Failed,'install exact retained M127 predecessor failed'):
                    work.qualify_upgrade(old,layout)
                self.assertEqual(work.phase,'install exact retained M127 predecessor')
                self.assertEqual(work.project,'ur-upgrade-'+layout+'-synthetic');self.assertEqual(work.directory,old.instances/layout)
                self.assertEqual(work.loopback_port,fixture.FIXTURE_PORTS[layout])
                self.assertEqual(old.loopback_port,work.loopback_port)
                self.assertEqual(work.result.get('installer_stop'),authored)

    def test_native_json_stdout_stays_separate_from_retained_startup_diagnostics(self):
        with tempfile.TemporaryDirectory() as tmp:
            root=Path(tmp);work=object.__new__(fixture.Acceptance)
            work.repo,work.logs,work.env=root,root/'logs',dict(fixture.os.environ);work.logs.mkdir()
            command=[fixture.sys.executable,'-I','-B','-c',
                'import sys; print(\'{"native":true}\'); print("native startup diagnostic",file=sys.stderr)']
            self.assertEqual(json.loads(work.run('actual stdout/stderr separation',command)),{'native':True})
            self.assertEqual((work.logs/'0.log.stderr').read_text(),'native startup diagnostic\n')
            self.assertTrue(all(path.stat().st_mode&0o777==0o600 for path in work.logs.iterdir()))
            command[-1]='import sys; print("public stdout"); print("authored failure",file=sys.stderr);sys.exit(7)'
            code,output=work.run('retain both failed streams',command,ok=False)
            self.assertEqual(code,7);self.assertIn('public stdout',output);self.assertIn('authored failure',output)

    def test_shipped_upgrade_authored_platform_refusal_is_reported_without_child_output(self):
        owner=load('actual_upgrade_error_boundary',ROOT/'deploy/customer/upgrade.py')
        errors=io.StringIO()
        with patch.object(owner.platform,'system',return_value='Darwin'), \
             patch.object(owner,'Installation',side_effect=AssertionError('must stop before opening installation')), \
             redirect_stderr(errors):
            self.assertEqual(owner.main(['upgrade']),1)
        self.assertEqual(errors.getvalue(),
            'Upgrade stopped: This upgrade entry supports Linux amd64\n'
            'Retain the original installation and upgrade evidence. No automatic reset or rollback was attempted.\n')
        with tempfile.TemporaryDirectory() as tmp:
            root=Path(tmp);work=object.__new__(fixture.Acceptance)
            work.repo,work.logs,work.release,work.env,work.result=root,root/'logs',root/'release',{},{}
            work.logs.mkdir()
            command=[fixture.sys.executable,'-I','-B',work.release/'payload/deploy/upgrade.py','upgrade']
            def refuse(args,**kwargs):
                kwargs['stdout'].write(b'synthetic-private-stdout\n')
                kwargs['stderr'].write(errors.getvalue().encode()+b'synthetic-private-stderr\n')
                return subprocess.CompletedProcess(args,1)
            with patch.object(fixture.subprocess,'run',side_effect=refuse):
                with self.assertRaises(fixture.Failed) as raised:work.run('actual authored refusal',command)
            self.assertEqual(str(raised.exception),
                'actual authored refusal failed: Upgrade stopped: This upgrade entry supports Linux amd64')
            self.assertNotIn('synthetic-private',str(raised.exception));self.assertEqual(work.result,{})
            self.assertTrue(all(path.stat().st_mode&0o777==0o600 for path in work.logs.iterdir()))

    def test_upgrade_diagnostic_excludes_other_entries_stdout_success_and_expected_refusals(self):
        with tempfile.TemporaryDirectory() as tmp:
            root=Path(tmp);work=object.__new__(fixture.Acceptance)
            work.repo,work.logs,work.release,work.env,work.result=root,root/'logs',root/'release',{},{}
            work.logs.mkdir()
            entry=str(work.release/'payload/deploy/upgrade.py')
            direct=[fixture.sys.executable,'-I','-B',entry,'resume-upgrade']
            marker=b'Upgrade stopped: synthetic-private-child-output\n'
            cases=[('wrong entry',[*direct[:3],str(root/'other/upgrade.py'),*direct[4:]],b'',marker,1,True),
                   ('path is hook data',[*direct[:3],str(root/'hook.py'),entry],b'',marker,1,True),
                   ('not isolated',[fixture.sys.executable,entry,'upgrade'],b'',marker,1,True),
                   ('stdout only',direct,marker,b'',1,True),
                   ('success',direct,b'{}\n',marker,0,True),
                   ('expected refusal',direct,b'',marker,1,False)]
            for label,command,out,err,code,ok in cases:
                with self.subTest(label=label):
                    def child(args,**kwargs):
                        kwargs['stdout'].write(out);kwargs['stderr'].write(err)
                        return subprocess.CompletedProcess(args,code)
                    with patch.object(fixture.subprocess,'run',side_effect=child):
                        if code and ok:
                            with self.assertRaises(fixture.Failed) as raised:work.run(label,command)
                            self.assertEqual(str(raised.exception),label+' failed')
                        else:
                            result=work.run(label,command,ok=ok)
                            self.assertEqual(result,out.decode() if ok else (code,(out+err).decode()))
                    self.assertEqual(work.result,{})

    def test_actual_install_execute_discards_secret_bearing_child_errors(self):
        owner=load('actual_upgrade_child_error_owner',ROOT/'deploy/customer/install.py')
        command=[fixture.sys.executable,'-I','-B','-c',
            'import sys;print("synthetic-private-child-output",file=sys.stderr);sys.exit(7)']
        with self.assertRaises(owner.InstallError) as raised:
            owner.execute(command,label='native upgrade owner: upgrade maintenance-check')
        self.assertEqual(str(raised.exception),
            'native upgrade owner: upgrade maintenance-check failed (exit 7); no secret-bearing child output was logged')

    def test_workflow_covers_selector_and_shared_first_use_inputs(self):
        # A change to an imported selector/assertion must exercise the real
        # native fixture, even when its main driver is otherwise unchanged.
        text = (ROOT / '.github/workflows/community-installer.yml').read_text()
        paths = text.split('    paths:\n', 1)[1].split('  workflow_dispatch:', 1)[0]
        for path in ('.github/tests/community-install-descriptor.py',
                     '.github/tests/test-community-install-descriptor.py',
                     '.github/tests/community-release-acceptance.py',
                     'cmd/uniroute/cli.go',
                     'cmd/uniroute/first_customer_upgrade_history_integration_test.go',
                     'internal/admin/first_customer_bootstrap_handlers.go',
                     'internal/admin/auth_handlers.go',
                     'internal/admin/audit_handlers.go'):
            self.assertIn('      - ' + path + '\n', paths)

    def raw(self, schema):
        return json.dumps({'schema_version': 'unirouter.community-bootstrap.v1',
            'platform': {'os': 'linux', 'arch': 'amd64'},
            'release': {'version': '1.13.0', 'commit': 'a' * 40, 'release_sequence': 1,
                        'key_id': 'installer-native-test', 'target_schema': schema},
            'assets': {key: {'url': 'https://example.invalid/' + key, 'size': 1,
                            'sha256': 'b' * 64} for key in bootstrap.ASSETS}}).encode()

    def test_fixture_matches_actual_latest_migration_and_both_install_owners(self):
        contract = fixture.source_contract(ROOT)
        self.assertEqual(contract.schema, bootstrap.SCHEMA)
        self.assertEqual(contract.schema, fixture.CURRENT_SCHEMA)
        self.assertGreater(len(contract.ledger), 120)
        self.assertEqual(contract.ledger[-1], contract.schema)
        self.assertIn(fixture.RETAINED_SCHEMA, contract.supported_heads)
        with self.assertRaises(AttributeError): contract.schema = fixture.RETAINED_SCHEMA

    def test_current_fixture_is_parsed_by_real_bootstrap_before_prepare(self):
        schema = fixture.source_contract(ROOT).schema
        raw = self.raw(schema)
        fetch = object()
        with patch.object(bootstrap, 'prepare') as prepare:
            fixture.prepare_bootstrap_fixture(bootstrap, raw, hashlib.sha256(raw).hexdigest(), Path('unused'), fetch)
        anchor = prepare.call_args.args[0]
        self.assertIsInstance(anchor, bootstrap.Bootstrap)
        self.assertEqual(anchor.release['target_schema'], schema)
        self.assertIs(prepare.call_args.kwargs['fetch'], fetch)

    def test_old_fixture_is_a_sanitized_failure_before_prepare(self):
        raw = self.raw('000126_commercial_license_retirement')
        with patch.object(bootstrap, 'prepare') as prepare:
            with self.assertRaisesRegex(fixture.Failed, '^community bootstrap preparation failed$'):
                fixture.prepare_bootstrap_fixture(bootstrap, raw, hashlib.sha256(raw).hexdigest(), Path('unused'), object())
        prepare.assert_not_called()

    def test_prepare_error_is_also_normalized_without_child_output(self):
        raw = self.raw(fixture.source_contract(ROOT).schema)
        with patch.object(bootstrap, 'prepare', side_effect=bootstrap.BootstrapError('untrusted child output')):
            with self.assertRaisesRegex(fixture.Failed, '^community bootstrap preparation failed$'):
                fixture.prepare_bootstrap_fixture(bootstrap, raw, hashlib.sha256(raw).hexdigest(), Path('unused'), object())

    def retained_metadata(self, root):
        git = ['git', '-C', str(ROOT)]
        names = ['deploy/customer/install.py', 'deploy/community/bootstrap.py', 'internal/schemaauthority/version.go']
        names += subprocess.check_output(git + ['ls-tree','-r','--name-only',fixture.RETAINED_COMMIT,'--','migrations']).decode().splitlines()
        for name in names:
            if name.startswith('migrations/') and not name.endswith('.up.sql'): continue
            path = root / name; path.parent.mkdir(parents=True,exist_ok=True)
            # The source-contract reader consumes the exact Git ledger names;
            # actual native ledger/SQL validation belongs to the Linux binary.
            path.write_bytes(b'' if name.startswith('migrations/') else
                             subprocess.check_output(git + ['show',fixture.RETAINED_COMMIT + ':' + name]))

    def test_retained_contract_reads_the_real_pinned_git_source(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp); self.retained_metadata(root)
            old = fixture.source_contract(root)
            self.assertEqual(old.schema, fixture.RETAINED_SCHEMA)
            self.assertEqual(old.ledger[-1], fixture.RETAINED_SCHEMA)
            self.assertNotIn(fixture.CURRENT_SCHEMA, old.supported_heads)
            self.assertEqual(fixture.source_contract(ROOT).ledger, old.ledger + (fixture.CURRENT_SCHEMA,))

    def test_schema_drift_is_rejected_before_any_runtime_operation(self):
        cases = ('bootstrap','native','ledger','unknown','nonliteral','duplicate')
        for case in cases:
            with self.subTest(case=case),tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp); self.retained_metadata(root)
                if case == 'ledger':
                    (root/'migrations'/ (fixture.CURRENT_SCHEMA+'.up.sql')).write_text('-- undeclared\n')
                else:
                    name = 'internal/schemaauthority/version.go' if case == 'native' else 'deploy/community/bootstrap.py'
                    path=root/name; raw=path.read_text()
                    if case == 'duplicate':raw+='\nSCHEMA = "'+fixture.RETAINED_SCHEMA+'"\n'
                    elif case == 'nonliteral':raw=raw.replace('SCHEMA = "'+fixture.RETAINED_SCHEMA+'"','SCHEMA = str(127)')
                    else:raw=raw.replace(fixture.RETAINED_SCHEMA, '000129_unapproved' if case == 'unknown' else fixture.CURRENT_SCHEMA)
                    path.write_text(raw)
                with patch.object(fixture.subprocess,'run',side_effect=AssertionError('must remain source-only')):
                    with self.assertRaises(fixture.Failed): fixture.source_contract(root)

    def exercise_release_preparation(self, repo, schema, version, commit):
        with tempfile.TemporaryDirectory() as tmp:
            root=Path(tmp); work=object.__new__(fixture.Acceptance)
            work.repo,work.root,work.release=repo,root,root/'release';work.release.mkdir()
            work.commit,work.tree,work.version=commit,'b'*40,version
            work.schema,work.release_sequence=schema,2 if schema==fixture.CURRENT_SCHEMA else 1
            work.verifier,work.ring=root/'verifier',root/'ring.json'
            work.verifier.write_bytes(b'synthetic verifier');work.ring.write_bytes(b'{}')
            work.images,work.image_ids,work.result={},{},{'checks':[]}
            commands=[]
            def run(label,args,**kwargs):
                args=[str(arg) for arg in args];commands.append((label,args))
                if label.startswith('inspect '):
                    reference=args[-1];image_repo=reference.split('@')[0] if '@' in reference else reference.rsplit(':',1)[0]
                    return json.dumps([{'Os':'linux','Architecture':'amd64','Id':'sha256:'+'c'*64,
                        'RepoDigests':[reference if '@' in reference else image_repo+'@sha256:'+'d'*64]}])
                if label=='pack synthetic signed release':
                    expected=json.loads(Path(args[args.index('--expected-artifacts')+1]).read_bytes())
                    paths={row['path']:row for row in expected}
                    for name in (('upgrade.py','recovery.py','install.py') if schema==fixture.CURRENT_SCHEMA else ('install.py',)):
                        p='payload/deploy/'+name;self.assertIn(p,paths)
                        self.assertEqual(paths[p]['sha256'],fixture.sha(repo/'deploy/customer'/name))
                    if schema==fixture.CURRENT_SCHEMA:
                        self.assertIn('payload/scripts/source-budget-restore-rebase.sql',paths)
                    else:
                        self.assertNotIn('payload/deploy/upgrade.py',paths)
                        raw=(work.release/'payload/deploy/docker-compose.yml').read_text()
                        self.assertEqual(raw.count(':/fixture-body'),1)
                        sample=(work.release/'payload/deploy/.env.example').read_text()
                        self.assertIn('SOURCE_CONTENT_STORAGE_ROOT=/fixture-body/objects\n',sample)
                        self.assertIn('initialize retained synthetic content through native owner',[label for label,_ in commands])
                    self.assertIn('--no-expiry',args);self.assertNotIn('--expires-at',args)
                    for flag,value in (('--release-sequence',str(work.release_sequence)),('--min-current-sequence','0'),
                        ('--max-current-sequence',str(work.release_sequence-1)),('--min-current-schema',fixture.RETAINED_SCHEMA),
                        ('--target-schema',schema)):
                        self.assertEqual(args[args.index(flag)+1],value)
                    Path(args[args.index('--out')+1]).write_bytes(b'synthetic-not-a-signed-package')
                return ''
            def prepare(module,raw,digest,directory,fetch):
                anchor=module.Bootstrap(raw,digest)
                self.assertEqual(anchor.release['target_schema'],schema)
                shutil.copytree(work.release,directory)
            work.run=run;work.docker='/fixture/docker';work.passed=lambda label:None
            with patch.object(fixture,'prepare_bootstrap_fixture',side_effect=prepare):
                work.prepare_release(root/'public-synthetic-key-only')
            builds=[args for label,args in commands if label.startswith('build exact')]
            self.assertEqual(len(builds),2)
            backend=next(args for args in builds if 'RUNTIME_COMMIT='+commit in args)
            self.assertIn('UNIROUTER_RELEASE_TRUST_MODE=release',backend)
            self.assertIn('UNIROUTER_UPGRADE_TRUST_BUNDLE_B64=e30',backend)

    def test_actual_release_preparation_carries_full_payload_and_native_presign_inventory(self):
        self.exercise_release_preparation(ROOT,fixture.CURRENT_SCHEMA,'1.15.0','a'*40)

    def test_retained_package_uses_same_trust_and_body_configuration_before_first_start(self):
        with tempfile.TemporaryDirectory() as tmp:
            source=Path(tmp)/'source'
            subprocess.run(['git','-C',str(ROOT),'clone','--shared','--no-checkout',str(ROOT),str(source)],
                check=True,stdout=subprocess.DEVNULL,stderr=subprocess.PIPE)
            subprocess.run(['git','-C',str(source),'checkout','--detach',fixture.RETAINED_COMMIT],
                check=True,stdout=subprocess.DEVNULL,stderr=subprocess.PIPE)
            self.exercise_release_preparation(source,fixture.RETAINED_SCHEMA,'1.14.2',fixture.RETAINED_COMMIT)

    def test_native_upgrade_arguments_use_the_shipped_owner_and_retained_predecessor(self):
        work=object.__new__(fixture.Acceptance);work.release=Path('/candidate');work.verifier=Path('/verifier');work.ring=Path('/ring')
        args=work.upgrade_command('resume-upgrade',Path('/original'),Path('/original/edge'),history=Path('/original/history.json'))
        self.assertEqual(args[3],Path('/candidate/payload/deploy/upgrade.py'))
        self.assertEqual(args[args.index('--source-package')+1],Path('/original/release.tar.gz'))
        self.assertEqual(args[args.index('--layout')+1],'ur_edge_pair')
        self.assertEqual(args[args.index('--historical-exits')+1],Path('/original/history.json'))

    def test_unknown_native_failure_never_waits_or_repeats_mutation(self):
        work=object.__new__(fixture.Acceptance);work.upgrade_command=lambda *args,**kwargs:['fixture']
        calls=[];work.run=lambda *args,**kw:(calls.append(args) or (1,'different native failure'))
        work.wait_stopped_leases=lambda *args:self.fail('unknown failure must not be retried')
        with self.assertRaisesRegex(fixture.Failed,'outside its known lease wait'):
            work.run_native_upgrade(Path('/original'))
        self.assertEqual(len(calls),1)

    def test_real_history_driver_captures_both_kinds_for_the_actual_shipped_validator(self):
        # Native effects are simulated only in this offline driver test. The
        # existing Linux cases call the same helper with actual Docker facts.
        for replacement in (False, True):
            with self.subTest(replacement=replacement),tempfile.TemporaryDirectory() as tmp:
                root=Path(tmp).resolve();directory=root/'installation';directory.mkdir();(directory/'evidence').mkdir()
                release=root/'release';shutil.copytree(ROOT/'deploy/customer',release/'payload/deploy',ignore=shutil.ignore_patterns('__pycache__'))
                owner=load('history_owner',release/'payload/deploy/upgrade.py')
                project='history-fixture';installation={'installation_id':'synthetic-installation','project':project}
                fixture.private_write(directory/'install-state.json',owner.canonical(installation))
                original='uri_'+'a'*32;successor='uri_'+'b'*32;image='sha256:'+'c'*64
                generation={'instance_id':original,'artifact_digest':image,'manifest_digest':'d'*64,
                    'compatibility_epoch':1,'schema_head':fixture.RETAINED_SCHEMA,'revision':7,
                    'state':'serving','ingress_state':'absent','lease_expires_at':'2000-01-01T00:00:00Z'}
                source={'release_sequence':1,'package_sha256':'e'*64}
                work=object.__new__(fixture.Acceptance);work.release=release;work.docker='/fixture/docker'
                work.target=lambda *args:{'system_identifier':'1','database':'uniroute','database_oid':1}
                def sql(_directory,_project,query):
                    if 'to_jsonb(g)' in query:return json.dumps(generation)
                    if 'greatest(0' in query:return '0'
                    if 'to_jsonb(s)' in query:return json.dumps(source)
                    self.fail('unexpected history query')
                work.sql=sql;calls=[];changed=False
                def run(label,args,**kwargs):
                    nonlocal changed
                    args=[str(arg) for arg in args];calls.append((label,args))
                    if 'inspect' in args:
                        cid='f'*64 if replacement and changed else 'a'*64
                        return json.dumps({'Id':cid,'Image':image,'State':{'Running':not changed or replacement,
                            'Pid':4321 if not changed or replacement else 0,'ExitCode':0,'OOMKilled':False}})
                    if 'exec' in args:
                        return json.dumps({'runtime':{'instance':successor if changed else original,'artifact_digest':image}})
                    if 'top' in args:return 'PID\n98765\n'
                    if 'stop' in args or 'up' in args:
                        changed=True
                        output='\n'.join(' Container '+project+'-backend-1 '+state for state in ('Recreated','Started','Healthy'))
                        return (0,output) if kwargs.get('ok') is False else output
                    self.fail('unexpected history command')
                work.run=run;old=SimpleNamespace(compose=lambda *args:[work.docker,'compose','--project-name',project])
                edge=root/'edge' if replacement else None
                with patch.object(owner.Upgrade,'_process_start',side_effect=lambda _:None if changed else '100'), \
                     patch.object(fixture.importlib.util,'spec_from_file_location',return_value=SimpleNamespace(loader=SimpleNamespace(exec_module=lambda _:None))), \
                     patch.object(fixture.importlib.util,'module_from_spec',return_value=owner):
                    path=work.historical_observations(old,directory,project,edge)
                evidence=owner.HistoricalObservations(path.read_bytes())
                actual=evidence.validate(installation,{'system_id':'1','database':'uniroute','database_oid':1,
                    'generations':[generation]},source,lambda _:None,lambda _:None)
                self.assertEqual(set(actual),{original})
                record=evidence.document['records'][0]
                self.assertEqual(record['kind'],'native_replacement' if replacement else 'process_exit')
                self.assertNotIn('clean_exit',record)
                self.assertEqual(len([args for label,args in calls if label=='perform exact native source history operation']),1)
                self.assertEqual(len([args for label,args in calls if label.startswith('restart exact')]),0 if replacement else 1)

    def test_session_logout_expiry_and_cleanup_use_exact_fixture_scope(self):
        work=object.__new__(fixture.Acceptance);work.result={};work.passed=lambda _:None
        work.loopback_port=fixture.FIXTURE_PORTS['edge']
        calls=[]
        def request(url,data=None,token=None):
            calls.append((url,token))
            if url.endswith('/auth/login'):return b'{"token":"public-synthetic-session"}'
            if url.endswith('/auth/logout'):return b'{}'
            raise fixture.urllib.error.HTTPError(url,401,'fixture refusal',{},None)
        sql=[];work.request=request;work.sql=lambda *args:sql.append(args[-1])
        work.session_refusals(Path('/fixture'),'fixture','public-synthetic-password')
        self.assertEqual(len([url for url,_ in calls if url.endswith('/bootstrap/first-customer')]),3)
        self.assertTrue(all(url.startswith('http://127.0.0.1:13002/') for url,_ in calls))
        self.assertEqual(len(sql),2)
        self.assertIn('expires_at<clock_timestamp()',sql[1])
        self.assertNotIn('public-synthetic-session',''.join(sql))
        self.assertTrue(work.result['session_refusals']['expired_fixture_session_removed'])

    def test_current_first_use_assertions_keep_case_port_through_backend_restart(self):
        with tempfile.TemporaryDirectory() as tmp:
            directory=Path(tmp);(directory/'runtime').mkdir();(directory/'evidence').mkdir()
            for path in ('admin-password','install-state.json','evidence/install.json'):
                (directory/path).write_text('public synthetic fixture')
            work=object.__new__(fixture.Acceptance);work.repo=Path('/retained-product-source')
            work.loopback_port=13001;work.docker='/fixture/docker';work.result={}
            work.sql=lambda *args:'{"admin":{"org_id":1},"projects":[{"id":2}]}'
            work.run=MagicMock();work.request=MagicMock()
            work.session_refusals=MagicMock();work.passed=lambda _:None
            first=MagicMock();first.Stop=RuntimeError;first.strict_json.side_effect=json.loads
            first.FIRST_USE_TEST_INPUT='public synthetic chosen input'
            first.immutable_digest.return_value='unchanged';first.first_customer_result.return_value=2
            first.fixture_admin_login.return_value={'token':'public synthetic session'}
            spec=SimpleNamespace(loader=SimpleNamespace(exec_module=lambda _:None))
            with patch.object(fixture.importlib.util,'spec_from_file_location',return_value=spec) as selection, \
                 patch.object(fixture.importlib.util,'module_from_spec',return_value=first):
                work.native_first_use_checks(directory,'fixture')
            self.assertEqual(selection.call_args.args[1],Path(fixture.__file__).with_name('community-release-acceptance.py'))
            self.assertEqual(first.fixture_admin_login.call_count,3)
            self.assertEqual(first.first_customer_request.call_count,3)
            for call in first.fixture_admin_login.call_args_list+first.first_customer_request.call_args_list:
                self.assertEqual(call.kwargs['loopback_port'],13001)
            work.request.assert_called_once_with('http://127.0.0.1:13001/health/ready')
            self.assertEqual(work.run.call_count,3)  # Password, native stop and native restart.

    def test_protected_projection_hashes_complete_original_rows_and_bounds_history(self):
        work=object.__new__(fixture.Acceptance); seen=[]
        names=('users','orgs','projects','wallets','transactions','project_members','audit_logs','release_history',
               'execution_content_objects','execution_content_streams')
        def fingerprint(*args,**kwargs):
            seen.append(kwargs['projections']);return {name:{'count':1,'sha256':name} for name in names}
        work.upgrade_fingerprint=fingerprint
        self.assertEqual(set(work.protected_upgrade_rows(Path('/fixture'),'fixture',1)),set(names))
        self.assertNotIn('users',seen[0]) # Default full to_jsonb row includes password identity/epoch.
        self.assertNotIn('wallets',seen[0]);self.assertNotIn('transactions',seen[0])
        self.assertIn("'bootstrap.first_customer'",seen[0]['audit_logs'][1])
        self.assertEqual(seen[0]['release_history'][1],' WHERE release_sequence<=1')

    def test_protected_configuration_reuses_real_metadata_inventory_and_detects_permission_drift(self):
        with tempfile.TemporaryDirectory() as tmp:
            root=Path(tmp).resolve();directory=root/'instance';directory.mkdir()
            (directory/'runtime').mkdir();(directory/'evidence').mkdir();body=root/'body';body.mkdir()
            for path in (directory/'runtime/.env',directory/'admin-password',directory/'release.tar.gz',directory/'evidence/install.json'):
                fixture.private_write(path,b'public synthetic fixture bytes')
            work=object.__new__(fixture.Acceptance)
            work.recovery_module=lambda:load('inventory_owner',ROOT/'deploy/customer/recovery.py')
            before=work.protected_upgrade_files(directory,body,None)
            fixture.private_write(directory/'runtime/upgrade-image.json',b'public synthetic new image override')
            self.assertEqual(before,work.protected_upgrade_files(directory,body,None))
            (directory/'runtime/.env').chmod(0o644)
            self.assertNotEqual(before,work.protected_upgrade_files(directory,body,None))

    def test_driver_never_starts_outside_its_disposable_runner(self):
        with patch.object(fixture.sys,'argv',['fixture','--expected-commit','a'*40,'--report','/not-created']), \
             patch.dict(fixture.os.environ,{},clear=True),patch.object(fixture.subprocess,'run',side_effect=AssertionError('must not run')):
            with self.assertRaisesRegex(fixture.Failed,'disposable GitHub-hosted'):
                fixture.main()

if __name__ == '__main__':
    unittest.main(verbosity=2)
