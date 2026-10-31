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
    def test_historical_content_uses_native_pairing_producer_and_retains_storage_identity(self):
        with tempfile.TemporaryDirectory() as tmp:
            root=Path(tmp);body=root/'historical-body';(body/'objects').mkdir(parents=True)
            (body/'binding.token').write_text('a'*64)
            statements=[]
            def sql(directory,project,query):
                statements.append(query)
                return '17' if query.startswith('INSERT INTO projects') else ''
            old=SimpleNamespace(root=root,result={'first_customer':{'organization_id':2}},sql=sql,
                compose=lambda *args:['not-executed'],run=lambda *args:'{"services":{}}')
            work=object.__new__(fixture.Acceptance)
            actual_body,name,rendered=work.historical_data(old,root/'instance','synthetic')
            self.assertEqual(actual_body,body);self.assertEqual(rendered,{'services':{}})
            self.assertIn("VALUES(2,17,'synthetic-source-control');",statements[1])
            self.assertIn("1,'community-linux-history','"+'a'*64+"'",statements[1])
            self.assertIn("'topup','completed',1234567,1234567",statements[1])
            self.assertEqual((body/'objects'/name).read_bytes(),
                b'Synthetic retained customer body; no provider or customer data.\n')
            self.assertEqual(fixture.sha(body/'objects'/name),name)

    def test_m128_observation_seed_validates_address_before_bound_go_command(self):
        for host in ('172.18.0.2','not-an-address','172.18.0.2 172.19.0.3'):
            with self.subTest(host=host):
                work=object.__new__(fixture.Acceptance);work.docker='/fixture/docker';work.env={}
                calls=[];target={'system_identifier':'synthetic-system','database':'uniroute','database_oid':17}
                old=SimpleNamespace(target=MagicMock(return_value=target))
                def run(label,args,**kwargs):
                    calls.append((label,args,kwargs))
                    if label=='read M128 synthetic database identity':
                        return json.dumps({'services':{'postgres':{'environment':{
                            'POSTGRES_USER':'fixture','POSTGRES_PASSWORD':'synthetic-unused','POSTGRES_DB':'uniroute'}}}})
                    if label=='read M128 private database address':return host
                    self.assertEqual(label,'seed typed nonempty M128 observation');return ''
                work.run=run
                if host!='172.18.0.2':
                    with self.assertRaises(ValueError):work.seed_native_observation(old,Path('/fixture/instance'),'synthetic')
                    self.assertEqual(len(calls),2);old.target.assert_not_called();continue
                work.seed_native_observation(old,Path('/fixture/instance'),'synthetic')
                _,args,kwargs=calls[-1]
                self.assertEqual(args,['go','test','-tags=integration','./cmd/uniroute','-run',
                    '^TestCommunityInstallerSeedM128ObservationIntegration$','-count=1','-timeout=90s'])
                self.assertTrue(all('synthetic-unused' not in str(arg) for arg in args))
                self.assertEqual(json.loads(kwargs['env']['UR_COMMUNITY_INSTALLER_SEED_TARGET']),target)
                self.assertEqual(kwargs['env']['UR_COMMUNITY_INSTALLER_SEED'],'observation-m128')
                self.assertEqual(kwargs['env']['REQUIRE_TEST_DATABASE'],'1')

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
        for failure in (None,'fresh','single','m128','same-schema'):
            with self.subTest(failure=failure):
                work=object.__new__(fixture.Acceptance);work.docker='/fixture/docker'
                work.instances=Path('/fixture/instances');work.suffix='synthetic';work.schema=fixture.CURRENT_SCHEMA
                work.result={'checks':[],'finite_upgrades':[]}
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
                def retained(*,observation=False):
                    if observation:
                        events.append('prepare:m128')
                        def first(*args,**kwargs):
                            self.assertTrue(kwargs['retain']);events.append('m127-to-m128')
                            return ('retained-m128',)
                        return SimpleNamespace(qualify_upgrade=first)
                    events.append('prepare:retained')
                    self.assertEqual(len(downs),1,'fresh network released before retained installation')
                    return SimpleNamespace()
                work.retained_release=retained
                def qualify(old,layout,**kwargs):
                    verify('m128' if kwargs.get('existing') else layout)
                    if kwargs.get('existing'):
                        self.assertTrue(kwargs['retain'])
                        return ('completed-m129',)
                    directory=Path('/fixture/retained')/layout
                    work.release_completed_case(directory,'ur-'+layout,directory/'edge' if layout=='edge' else None,upgraded=True)
                work.qualify_upgrade=qualify
                def same_schema():
                    events.append('repack:same-images')
                    following=SimpleNamespace(result={'checks':[],'finite_upgrades':[{'mode':'same_schema'}]},phase='same-schema')
                    def upgrade(old,layout,**kwargs):
                        self.assertIs(old,work);self.assertEqual(kwargs,{'existing':('completed-m129',)})
                        verify('same-schema')
                        work.release_completed_case(Path('/fixture/retained/single'),'ur-single',upgraded=True)
                    following.qualify_upgrade=upgrade
                    return following
                work.same_schema_release=same_schema
                if failure:
                    with self.assertRaisesRegex(fixture.Failed,'preserve failed case'):work.execute()
                    self.assertEqual(len(downs),{'fresh':0,'single':1,'m128':4,'same-schema':4}[failure])
                    self.assertNotEqual(work.result.get('status'),'passed')
                else:
                    work.execute()
                    self.assertEqual(events,['fresh','down:ur-native-synthetic','prepare:retained','single','down:ur-single',
                        'edge','down:ur-edge-edge','down:ur-edge','prepare:m128','m127-to-m128','m128',
                        'repack:same-images','same-schema','down:ur-single'])
                    self.assertEqual(work.result['status'],'passed')
                    self.assertEqual(work.result['finite_upgrades'],[{'mode':'same_schema'}])
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

    def retained_metadata(self, root, commit=fixture.RETAINED_COMMIT):
        git = ['git', '-C', str(ROOT)]
        names = ['deploy/customer/install.py', 'deploy/community/bootstrap.py', 'internal/schemaauthority/version.go']
        names += subprocess.check_output(git + ['ls-tree','-r','--name-only',commit,'--','migrations']).decode().splitlines()
        for name in names:
            if name.startswith('migrations/') and not name.endswith('.up.sql'): continue
            path = root / name; path.parent.mkdir(parents=True,exist_ok=True)
            # The source-contract reader consumes the exact Git ledger names;
            # actual native ledger/SQL validation belongs to the Linux binary.
            path.write_bytes(b'' if name.startswith('migrations/') else
                             subprocess.check_output(git + ['show',commit + ':' + name]))

    def test_retained_contract_reads_the_real_pinned_git_source(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp); self.retained_metadata(root)
            old = fixture.source_contract(root)
            self.assertEqual(old.schema, fixture.RETAINED_SCHEMA)
            self.assertEqual(old.ledger[-1], fixture.RETAINED_SCHEMA)
            self.assertNotIn(fixture.CURRENT_SCHEMA, old.supported_heads)
            self.assertEqual(fixture.source_contract(ROOT).ledger, old.ledger + (fixture.OBSERVATION_SCHEMA,fixture.CURRENT_SCHEMA,))

    def test_retained_m128_contract_reads_the_actual_original_application_source(self):
        with tempfile.TemporaryDirectory() as tmp:
            root=Path(tmp);self.retained_metadata(root,fixture.OBSERVATION_COMMIT)
            old=fixture.source_contract(root)
            self.assertEqual(old.schema,fixture.OBSERVATION_SCHEMA)
            self.assertEqual(fixture.source_contract(ROOT).ledger,old.ledger+(fixture.CURRENT_SCHEMA,))
            self.assertNotIn(fixture.CURRENT_SCHEMA,old.supported_heads)
            self.assertEqual(subprocess.check_output(['git','-C',str(ROOT),'show',fixture.OBSERVATION_COMMIT+':VERSION']).decode().strip(),'1.15.0')

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

    def exercise_release_preparation(self, repo, schema, version, commit, *, same_schema=False):
        with tempfile.TemporaryDirectory() as tmp:
            root=Path(tmp); work=object.__new__(fixture.Acceptance)
            work.repo,work.root,work.release=repo,root,root/'release';work.release.mkdir()
            work.commit,work.tree,work.version=commit,'b'*40,version
            work.schema,work.release_sequence=schema,{fixture.RETAINED_SCHEMA:1,fixture.OBSERVATION_SCHEMA:2,fixture.CURRENT_SCHEMA:3}[schema]
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
                    for name in (('upgrade.py','recovery.py','install.py') if schema in (fixture.OBSERVATION_SCHEMA,fixture.CURRENT_SCHEMA) else ('install.py',)):
                        p='payload/deploy/'+name;self.assertIn(p,paths)
                        self.assertEqual(paths[p]['sha256'],fixture.sha(repo/'deploy/customer'/name))
                    if schema in (fixture.OBSERVATION_SCHEMA,fixture.CURRENT_SCHEMA):
                        self.assertIn('payload/scripts/source-budget-restore-rebase.sql',paths)
                    else:
                        self.assertNotIn('payload/deploy/upgrade.py',paths)
                        raw=(work.release/'payload/deploy/docker-compose.yml').read_text()
                        self.assertEqual(raw.count(':/fixture-body'),1)
                        sample=(work.release/'payload/deploy/.env.example').read_text()
                        self.assertIn('SOURCE_CONTENT_STORAGE_ROOT=/fixture-body/objects\n',sample)
                        self.assertIn('initialize retained synthetic content through native owner',[label for label,_ in commands])
                    self.assertIn('--no-expiry',args);self.assertNotIn('--expires-at',args)
                    sequence=int(args[args.index('--release-sequence')+1])
                    self.assertIn(sequence,(work.release_sequence,work.release_sequence+1) if same_schema else (work.release_sequence,))
                    for flag,value in (('--min-current-sequence','0'),
                        ('--max-current-sequence',str(sequence-1)),('--min-current-schema',fixture.RETAINED_SCHEMA),
                        ('--target-schema',schema)):
                        self.assertEqual(args[args.index(flag)+1],value)
                    Path(args[args.index('--out')+1]).write_bytes(b'synthetic-not-a-signed-package-'+str(sequence).encode())
                return ''
            def prepare(module,raw,digest,directory,fetch):
                anchor=module.Bootstrap(raw,digest)
                self.assertEqual(anchor.release['target_schema'],schema)
                shutil.copytree(directory.parent/'release',directory)
            work.run=run;work.docker='/fixture/docker';work.passed=lambda label:None
            with patch.object(fixture,'prepare_bootstrap_fixture',side_effect=prepare):
                work.prepare_release(root/'test.private')
                if same_schema:
                    before=fixture.files(work.release)
                    following=work.same_schema_release()
                    self.assertEqual(fixture.files(work.release),before)
                    self.assertEqual(following.release_sequence,4)
                    self.assertIs(following.ring,work.ring);self.assertIs(following.verifier,work.verifier)
                    self.assertIs(following.images,work.images);self.assertIs(following.image_ids,work.image_ids)
                    packs=[args for label,args in commands if label=='pack synthetic signed release']
                    self.assertEqual([args[args.index('--release-sequence')+1] for args in packs],['3','4'])
                    self.assertTrue(all(args[args.index('--private-key')+1]==str(root/'test.private') for args in packs[1:]))
                    # Execute only the observer with harmless Python children.
                    # This proves its real subprocess hook, not Linux upgrade.
                    program=root/'observer-contract.py'
                    program.write_text('import subprocess,sys\nsubprocess.run([sys.executable,"-c","pass","./uniroute",*sys.argv[1:3],"synthetic-secret-must-not-be-retained"],check=True)\nsys.exit(int(sys.argv[3]))\n')
                    for verb,operation,exit_code in (('runtime-generation','inspect',0),('migrate','up',7)):
                        result=subprocess.run([fixture.sys.executable,'-I','-B',str(following.migration_observer),
                            str(following.migration_audit),str(program),verb,operation,str(exit_code)],capture_output=True)
                        self.assertEqual(result.returncode,exit_code,result.stderr)
                    raw=following.migration_audit.read_text()
                    self.assertEqual([json.loads(line) for line in raw.splitlines()],
                        [{'native_owner_calls':1,'migration_owner_calls':0},{'native_owner_calls':1,'migration_owner_calls':1}])
                    self.assertNotIn('synthetic-secret',raw)
                    self.assertEqual(following.migration_audit.stat().st_mode&0o777,0o600)
            builds=[args for label,args in commands if label.startswith('build exact')]
            self.assertEqual(len(builds),2)
            backend=next(args for args in builds if 'RUNTIME_COMMIT='+commit in args)
            self.assertIn('UNIROUTER_RELEASE_TRUST_MODE=release',backend)
            self.assertIn('UNIROUTER_UPGRADE_TRUST_BUNDLE_B64=e30',backend)

    def test_actual_release_preparation_carries_full_payload_and_native_presign_inventory(self):
        self.exercise_release_preparation(ROOT,fixture.CURRENT_SCHEMA,'1.15.0','a'*40)

    def test_same_schema_repack_reuses_exact_images_payload_key_and_native_command_observer(self):
        self.exercise_release_preparation(ROOT,fixture.CURRENT_SCHEMA,'1.15.2','a'*40,same_schema=True)

    def test_same_schema_qualification_retains_native_contract_and_refuses_proof_drift(self):
        # Simulate only native effects here. The real Linux entry calls this
        # exact qualification driver on the completed populated M129 instance.
        for drift in (None,'mode','ledger','ledger-row','migration','original-receipt','source-snapshot','resume','read-only','candidate'):
            with self.subTest(drift=drift),tempfile.TemporaryDirectory() as tmp:
                root=Path(tmp).resolve();directory=root/'instance';directory.mkdir()
                for name in ('runtime','evidence','upgrades/prior','releases'):
                    (directory/name).mkdir(parents=True)
                for name in ('runtime/.env','admin-password','release.tar.gz','evidence/install.json','upgrades/prior/receipt.json','releases/prior.tar.gz'):
                    fixture.private_write(directory/name,b'synthetic original bytes')
                body=root/'body';(body/'objects').mkdir(parents=True)
                name=hashlib.sha256(b'synthetic historical body').hexdigest()
                fixture.private_write(body/'objects'/name,b'synthetic historical body')
                work=object.__new__(fixture.Acceptance)
                work.root,work.instances,work.suffix=root,root/'instances','synthetic'
                work.release=root/'candidate';work.release.mkdir()
                fixture.private_write(work.release/'release.tar.gz',b'synthetic next package')
                source=root/'source';source.mkdir();fixture.private_write(source/'release.tar.gz',b'synthetic current package')
                work.schema=fixture.CURRENT_SCHEMA;work.release_sequence=4
                work.source_contract=fixture.source_contract(ROOT);ledger=list(work.source_contract.ledger)
                work.verifier,work.ring=root/'verifier',root/'public-ring'
                work.result={};work.migration_audit=root/'migration-owner-audit.jsonl'
                checked={'writes':0,'writers':1,'source':{'release_sequence':3,'package_sha256':fixture.sha(source/'release.tar.gz')},
                    'candidate':{'release_sequence':4,'package_sha256':fixture.sha(work.release/'release.tar.gz'),'version':'1.15.2','commit':'a'*40}}
                prior=dict(checked['candidate'],release_sequence=3,package_sha256=checked['source']['package_sha256'])
                original=json.dumps({'upgrade':{'candidate':prior,'state':'completed','pending':None},'current_release':prior}).encode()
                fixture.private_write(directory/'install-state.json',original)
                recovery=load('same_schema_fixture_inventory',ROOT/'deploy/customer/recovery.py')
                work.recovery_module=lambda:recovery
                tables={table:{'count':1,'sha256':'b'*64} for table in ('users','orgs','projects','wallets','transactions',
                    'project_members','audit_logs','release_history','execution_content_objects','execution_content_streams','model_deployments','schema_migrations')}
                native_done=False
                def fingerprint(*args,**kwargs):
                    return dict(tables,schema_migrations={'count':len(ledger),'sha256':'changed-ledger-row'}) if drift=='ledger-row' and native_done else tables
                work.upgrade_fingerprint=fingerprint
                work.inventory=lambda:{'unchanged-original-resources':True}
                def sql(_directory,_project,query,database='uniroute'):
                    if 'FROM schema_migrations' in query:return json.dumps(ledger)
                    if 'FROM release_history' in query:return '4'
                    if 'count(*)' in query:return '1'
                    self.fail('unexpected fixture SQL')
                work.sql=sql
                old=SimpleNamespace(schema=fixture.CURRENT_SCHEMA,release_sequence=3,release=source,instances=work.instances,sql=lambda *args:'0')
                history=directory/'evidence/history.json';fixture.private_write(history,b'original observation')
                work.historical_observations=lambda *args:history
                work.seed_native_pair=MagicMock(side_effect=AssertionError('must reuse existing pairing'))
                work.seed_native_observation=MagicMock(side_effect=AssertionError('must reuse existing observation'))
                work.interrupt_startup_hold=MagicMock(side_effect=AssertionError('must not repeat real timer wait'))
                work.verify_released_cleanup=MagicMock(side_effect=AssertionError('must reuse timer coverage'))
                calls=[]
                def run(label,args,**kwargs):
                    calls.append(label)
                    if label=='refuse original history without its observations':return 1,'unaccounted or missing writer'
                    if label=='check exact supported source without effects':
                        result=json.loads(json.dumps(checked))
                        if drift=='read-only':result['writes']=1
                        if drift=='candidate':result['candidate']['package_sha256']='0'*64
                        return json.dumps(result)
                    if label=='read actual source upgrade status':return json.dumps({'state':'completed','candidate':prior})
                    if label=='read back completed native upgrade resume':
                        if drift=='resume':(directory/'install-state.json').write_bytes((directory/'install-state.json').read_bytes()+b' ')
                        return '{}'
                    self.fail('unexpected child command: '+label)
                work.run=run
                def native(*args):
                    nonlocal native_done
                    native_done=True
                    calls.append('normal-native-owner')
                    operation=directory/'upgrades/next';backup=operation/'backup';backup.mkdir(parents=True)
                    fixture.private_write(operation/'source-install-state.json',b'changed source snapshot' if drift=='source-snapshot' else original)
                    fixture.private_write(backup/'receipt.json',json.dumps({'verification_database':'independent-verification',
                        'restored_tables':tables,'tables':tables}).encode())
                    restored=backup/'restore-files'/str(body/'objects'/name).lstrip('/')
                    restored.parent.mkdir(parents=True);fixture.private_write(restored,b'synthetic historical body')
                    witness={'state':'completed','pending':None,'operation_id':'next','source_state_path':'upgrades/next/source-install-state.json',
                        'candidate':checked['candidate'],'source':checked['source'],'source_ledger':ledger[:-1] if drift=='ledger' else ledger,
                        'receipts':{'migrate':{'mode':'wrong' if drift=='mode' else 'same_schema',
                            'ledger_sha256':hashlib.sha256(json.dumps(ledger,sort_keys=True,separators=(',',':')).encode()).hexdigest()},
                            'stop-backend':{'clean_exit':True,'old_processes_gone':True},
                            'stop-frontend':{'clean_exit':True,'old_processes_gone':True}}}
                    (directory/'install-state.json').write_text(json.dumps({'upgrade':witness}))
                    if drift=='original-receipt':(directory/'upgrades/prior/receipt.json').write_bytes(b'changed old receipt')
                    fixture.private_write(work.migration_audit,json.dumps({'native_owner_calls':1,
                        'migration_owner_calls':1 if drift=='migration' else 0}).encode()+b'\n')
                work.run_native_upgrade=native
                work.request=MagicMock(return_value=b'{"organization_role":"platform_admin"}')
                work.wait_business_startup=MagicMock();work.passed=MagicMock();work.release_completed_case=MagicMock()
                existing=(directory,'original-project',body,name,{})
                with patch.object(fixture.subprocess,'run',side_effect=AssertionError('offline driver must not launch services')):
                    if drift:
                        with self.assertRaises(fixture.Failed):work.qualify_upgrade(old,'single',existing=existing)
                        work.release_completed_case.assert_not_called()
                    else:
                        work.qualify_upgrade(old,'single',existing=existing)
                        result=work.result['finite_upgrades'][-1]
                        self.assertEqual((result['source_sequence'],result['candidate_sequence']),(3,4))
                        self.assertEqual(result['mode'],'same_schema');self.assertEqual(result['migration_owner_calls'],0)
                        self.assertTrue(result['complete_ledger_unchanged']);self.assertTrue(result['original_operation_evidence_unchanged'])
                        self.assertEqual(calls.count('normal-native-owner'),1)
                        self.assertEqual(calls.count('read back completed native upgrade resume'),1)
                        work.release_completed_case.assert_called_once_with(directory,'original-project',None,upgraded=True)
                        self.assertEqual((directory/'upgrades/prior/receipt.json').read_bytes(),b'synthetic original bytes')
                        self.assertEqual((directory/'upgrades/next/source-install-state.json').read_bytes(),original)
                work.seed_native_pair.assert_not_called();work.seed_native_observation.assert_not_called()
                work.interrupt_startup_hold.assert_not_called();work.verify_released_cleanup.assert_not_called()

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
        self.assertNotIn('--source-package',args,'ordinary owner must choose completed current_release, not the historical origin')
        self.assertEqual(args[args.index('--layout')+1],'ur_edge_pair')
        self.assertEqual(args[args.index('--historical-exits')+1],Path('/original/history.json'))

    def test_native_upgrade_uses_one_safe_error_boundary_for_all_control_flow(self):
        lease="Upgrade stopped: A stopped writer's original native lease is still live; wait and read back before retiring"
        unknown='Upgrade stopped: This upgrade entry supports Linux amd64'
        resume_error='Upgrade stopped: Native retirement commit is unconfirmed'
        cases=[('unknown initial refusal',[(1,unknown)],['upgrade'],'run actual shipped upgrade failed: '+unknown),
               ('known lease then success',[(1,lease),(0,'')],['upgrade','wait','resume-upgrade'],None),
               ('known lease then resume refusal',[(1,lease),(1,resume_error)],['upgrade','wait','resume-upgrade'],
                'resume after proved native lease expiry failed: '+resume_error),
               ('initial success',[(0,'')],['upgrade'],None)]
        for observed,label,replies,expected_calls,expected_error in ((observed,*case) for observed in (False,True) for case in cases):
            with self.subTest(label=label,observed=observed),tempfile.TemporaryDirectory() as tmp:
                root=Path(tmp);work=object.__new__(fixture.Acceptance)
                work.repo,work.logs,work.release,work.env,work.result=root,root/'logs',root/'release',{},{}
                work.logs.mkdir();work.verifier=root/'verifier';work.ring=root/'public-ring'
                if observed:
                    work.migration_observer=root/'observe-migration-owner.py';work.migration_audit=root/'audit.jsonl'
                calls=[];remaining=list(replies)
                def child(argv,**kwargs):
                    prefix=[fixture.sys.executable,'-I','-B']
                    if observed:prefix.extend([str(work.migration_observer),str(work.migration_audit)])
                    prefix.append(str(work.release/'payload/deploy/upgrade.py'))
                    self.assertEqual(argv[:len(prefix)],prefix)
                    calls.append(argv[len(prefix)]);code,error=remaining.pop(0)
                    kwargs['stdout'].write(b'synthetic-private-stdout\n' if code else b'{"state":"completed"}\n')
                    kwargs['stderr'].write((error+'\nsynthetic-private-stderr\n').encode())
                    return subprocess.CompletedProcess(argv,code)
                work.wait_stopped_leases=lambda directory:calls.append('wait')
                with patch.object(fixture.subprocess,'run',side_effect=child):
                    if expected_error:
                        with self.assertRaises(fixture.Failed) as raised:work.run_native_upgrade(root/'installation')
                        self.assertEqual(str(raised.exception),expected_error)
                        self.assertNotIn('synthetic-private',str(raised.exception))
                    else:work.run_native_upgrade(root/'installation')
                self.assertEqual(calls,expected_calls);self.assertEqual(remaining,[]);self.assertEqual(work.result,{})

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
                        output='\n'.join(' Container '+project+'-backend-1  '+state for state in ('Recreated','Started','Healthy'))
                        return (0,output) if kwargs.get('ok') is False else output
                    self.fail('unexpected history command')
                work.run=run;old=SimpleNamespace(compose=lambda *args:[work.docker,'compose','--project-name',project],release_sequence=1)
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
            raise fixture.urllib.error.HTTPError(url,401,'fixture refusal',{},io.BytesIO())
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
