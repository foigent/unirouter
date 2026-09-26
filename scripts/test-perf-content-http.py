#!/usr/bin/env python3
"""Offline driver regression: loopback synthetic owners only; no PG/Redis/Provider."""
import concurrent.futures
import importlib.util
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import threading
import time
import unittest
from unittest.mock import patch, Mock

DRIVER = Path(__file__).with_name('perf-content-http.py')
spec = importlib.util.spec_from_file_location('perf_content_http', DRIVER)
driver = importlib.util.module_from_spec(spec)
spec.loader.exec_module(driver)

# A real child and loopback listener, with no database or external calls. The
# production Go fixture is never launched by this suite.
BACKEND = r'''
import http.server,json,os,pathlib,threading,time,sys
root=pathlib.Path(os.environ['UR_CONTENT_PERFORMANCE_OUTPUT'])
if os.environ.get('CC13_FAKE_START_FAIL')=='1':sys.exit(7)
class H(http.server.BaseHTTPRequestHandler):
 def log_message(self,*args):pass
 def do_POST(self):
  if self.path=='/__fixture/finish':
   (root/'finish-observed').write_text('yes')
   self.send_response(500 if os.environ.get('CC13_FAKE_FINISH_FAIL')=='1' else 202);self.send_header('Content-Length','0');self.end_headers()
   threading.Thread(target=server.shutdown).start();return
  data=json.loads(self.rfile.read(int(self.headers['Content-Length'])))
  marker=data['messages'][0]['content'].split('|')[0]
  if 'plain' in marker:
   (root/'load-started').touch()
   time.sleep(.15)
  body=json.dumps({'choices':[{'message':{'content':'s'*8192}}]}).encode()
  self.send_response(200);self.send_header('X-Request-ID',marker);self.send_header('Content-Length',str(len(body)));self.end_headers();self.wfile.write(body)
server=http.server.ThreadingHTTPServer(('127.0.0.1',0),H)
(root/'ready.json').write_text(json.dumps({'url':'http://127.0.0.1:'+str(server.server_port),'pid':os.getpid()}))
server.serve_forever();server.server_close()
time.sleep(float(os.environ.get('CC13_FAKE_EXIT_DELAY','0')))
(root/'owner-exited').write_text('yes')
'''

RUNNER = r'''
import importlib.util,pathlib,sys,time,json
spec=importlib.util.spec_from_file_location('driver',sys.argv[1]);d=importlib.util.module_from_spec(spec);spec.loader.exec_module(d)
d.process_sample=lambda pid:{'cpu_seconds':0,'monotonic':time.monotonic(),'VmRSS_bytes':0,'VmHWM_bytes':0}
if sys.argv[4]=='wait':
 d.window=lambda *args,**kwargs:None
 original_popen=d.subprocess.Popen
 def popen(*args,**kwargs):
  child=original_popen(*args,**kwargs);original_wait=child.wait;budgets=[]
  def wait(*args,**kwargs):
   budgets.append(kwargs.get('timeout'));(pathlib.Path(sys.argv[3])/'wait-budgets.json').write_text(json.dumps(budgets))
   (pathlib.Path(sys.argv[3])/'wait-entered').touch()
   return original_wait(*args,**kwargs)
  child.wait=wait;return child
 d.subprocess.Popen=popen
if sys.argv[4]=='exception':
 def fail(*args,**kwargs):raise RuntimeError('synthetic_window_failure')
 d.window=fail
try:d.run_round(pathlib.Path(sys.argv[2]),pathlib.Path(sys.argv[3]),'disabled',0,'http://127.0.0.1:1',True)
except BaseException:sys.exit(23)
'''

class DriverTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix='cc13-offline-')
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.binary = self.root/'fake-go'
        self.binary.write_text('#!'+sys.executable+'\n'+BACKEND)
        self.binary.chmod(0o700)
        self.output = self.root/'evidence';self.output.mkdir()
        self.metric = lambda pid: {'cpu_seconds':0, 'monotonic':time.monotonic(), 'VmRSS_bytes':0,'VmHWM_bytes':0}
        self.addCleanup(self.assert_no_sampler)

    def assert_no_sampler(self):
        self.assertFalse([t.name for t in threading.enumerate() if t.name.startswith('cc13-')])

    def child_run(self, mode='exception', extra=None, interrupt=False):
        env = dict(os.environ, REQUIRE_TEST_DATABASE='0', TEST_DATABASE_URL='postgres://disabled:disabled@127.0.0.1:1/disabled?sslmode=disable', UR_CONTENT_PERFORMANCE='0')
        env.update(extra or {})
        proc = subprocess.Popen([sys.executable,'-c',RUNNER,str(DRIVER),str(self.binary),str(self.output),mode],env=env,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        try:
            if interrupt:
                deadline=time.monotonic()+10
                marker=self.output/('wait-entered' if mode=='wait' else '00-disabled/load-started')
                while not marker.exists() and proc.poll() is None and time.monotonic()<deadline:
                    time.sleep(.01)
                self.assertTrue(marker.exists(), 'real load must have started before SIGINT')
                proc.send_signal(signal.SIGINT)
            stdout,stderr=proc.communicate(timeout=15)
        except BaseException:
            proc.kill();proc.communicate();raise
        result=json.loads((self.output/'00-disabled/result.json').read_text())
        self.assertTrue(result['backend_reaped'])
        self.assertFalse(result['backend_alive'])
        # Confirm the recorded PID no longer denotes the child owner.
        with self.assertRaises(ProcessLookupError):os.kill(result['backend_pid'],0)
        return proc.returncode,result

    def test_real_sigint_saves_partial_window_and_reaps_owner(self):
        code,result=self.child_run('signal',interrupt=True)
        self.assertEqual(code,23);self.assertEqual(result['error'],'KeyboardInterrupt')
        window=result['windows'][0]
        self.assertTrue(window['interrupted']);self.assertGreater(window['completed'],0)
        self.assertEqual(window['requested'],window['completed']+window['cancelled']+window['unsubmitted']+len(window['future_errors']))
        self.assertGreater(window['unsubmitted'],0)
        self.assertTrue((self.output/'00-disabled/finish-observed').exists())
        self.assertTrue((self.output/'00-disabled/owner-exited').exists())
        self.assertFalse((self.output/'00-disabled/stream.summary.json').exists())

    def test_real_sigint_during_wait_preserves_owner_reap(self):
        code,result=self.child_run('wait',extra={'CC13_FAKE_EXIT_DELAY':'1'},interrupt=True)
        self.assertEqual(code,0)
        self.assertTrue(result['backend_reaped'])
        self.assertIn('backend_wait_interrupt',[e['step'] for e in result['cleanup_errors']])
        self.assertFalse(driver.round_success(result))
        budgets=json.loads((self.output/'wait-budgets.json').read_text())
        self.assertEqual(len(budgets),2);self.assertLess(budgets[1],budgets[0]);self.assertLessEqual(budgets[0],100)
        self.assertTrue((self.output/'00-disabled/owner-exited').exists())

    def test_window_exception_still_finishes_and_reaps(self):
        code,result=self.child_run()
        self.assertEqual(code,23);self.assertEqual(result['error'],'RuntimeError')
        self.assertTrue((self.output/'00-disabled/warmup.json').exists())

    def test_finish_failure_does_not_skip_reap(self):
        _,result=self.child_run(extra={'CC13_FAKE_FINISH_FAIL':'1'})
        self.assertIn('finish',[e['step'] for e in result['cleanup_errors']])
        self.assertTrue((self.output/'00-disabled/owner-exited').exists())

    def test_backend_start_failure_is_reaped(self):
        _,result=self.child_run(extra={'CC13_FAKE_START_FAIL':'1'})
        self.assertEqual(result['exit_code'],7)
        self.assertEqual(result['warmup_requests'],0)

    def test_sampler_failure_is_recorded_and_stopped(self):
        with patch.object(driver,'process_sample',side_effect=OSError('synthetic')),patch.object(driver,'request',return_value=self.sample()):
            summary=driver.window(None,123,'offline',False,1,1,self.output/'plain')
        self.assertEqual(summary['sampler_errors'],['OSError'])
        self.assertIsNone(summary['backend_cpu_percent_one_core'])

    def test_completed_unconsumed_futures_survive_interrupt(self):
        def interrupt(futures):
            concurrent.futures.wait(futures)
            raise KeyboardInterrupt()
        with patch.object(driver,'process_sample',self.metric),patch.object(driver,'request',return_value=self.sample()),patch.object(driver.concurrent.futures,'as_completed',side_effect=interrupt):
            with self.assertRaises(KeyboardInterrupt):driver.window(None,123,'offline',False,1,1,self.output/'plain')
        summary=json.loads((self.output/'plain.summary.json').read_text())
        self.assertEqual(summary['completed'],1);self.assertTrue(summary['interrupted'])

    def test_write_failure_does_not_skip_owner_cleanup(self):
        original=driver.write
        def write(path,value):
            if path.name=='warmup.json':raise OSError('synthetic')
            original(path,value)
        with patch.object(driver,'process_sample',self.metric),patch.object(driver,'window',side_effect=RuntimeError('synthetic')),patch.object(driver,'write',side_effect=write):
            with self.assertRaises(RuntimeError):driver.run_round(self.binary,self.output,'disabled',0,'http://127.0.0.1:1',True)
        result=json.loads((self.output/'00-disabled/result.json').read_text())
        self.assertTrue(result['backend_reaped']);self.assertIn('write_warmup.json',[e['step'] for e in result['cleanup_errors']])

    def test_http_provider_classification_preserves_failed_execution(self):
        samples=[dict(status=409,error='http_status',error_response='{"error":{"code":"provider_cost_pending"}}',provider={'calls':0}),dict(status=503,error='http_status',provider={'calls':1})]
        groups=driver.classify_samples(samples)
        self.assertEqual(sum(g['count'] for g in groups),2)
        self.assertIn(1,[g['provider_calls'] for g in groups])

    def test_wait_timeout_reports_live_owner_without_kill(self):
        owner=Mock(pid=123456)
        owner.poll.return_value=None
        owner.wait.side_effect=subprocess.TimeoutExpired('synthetic',100)
        output=self.output/'00-disabled'
        original_mkdir=Path.mkdir
        def mkdir(path,*args,**kwargs):
            original_mkdir(path,*args,**kwargs)
            if path==output:
                (path/'ready.json').write_text(json.dumps({'url':'http://127.0.0.1:1'}))
        connection=Mock()
        connection.getresponse.return_value.status=202
        with patch.object(Path,'mkdir',mkdir),patch.object(driver.subprocess,'Popen',return_value=owner),patch.object(driver.http.client,'HTTPConnection',return_value=connection),patch.object(driver,'process_sample',self.metric),patch.object(driver,'request',side_effect=RuntimeError('synthetic')):
            with self.assertRaises(RuntimeError):driver.run_round(self.binary,self.output,'disabled',0,'http://127.0.0.1:1',True)
        result=json.loads((output/'result.json').read_text())
        owner.wait.assert_called_once()
        self.assertGreater(owner.wait.call_args.kwargs['timeout'],0)
        self.assertLessEqual(owner.wait.call_args.kwargs['timeout'],100)
        owner.kill.assert_not_called();owner.terminate.assert_not_called()
        self.assertFalse(result['backend_reaped']);self.assertTrue(result['backend_alive'])
        self.assertIsNone(result['exit_code'])
        self.assertIn('backend_wait',[e['step'] for e in result['cleanup_errors']])

    def test_first_failed_round_stops_sequence(self):
        result={'exit_code':1,'integrity':None}
        with patch.object(driver,'run_round',return_value=result) as run:
            results=driver.run_sequence(self.binary,self.output,'http://127.0.0.1:1',False)
        self.assertEqual(len(results),1);self.assertEqual(run.call_count,1)

    def test_fixed_mode_preserves_full_load_and_omits_other_modes(self):
        with patch.object(driver,'run_round',return_value={}) as run,patch.object(driver,'round_success',return_value=True):
            results=driver.run_sequence(self.binary,self.output,'http://127.0.0.1:1',False,'disabled')
        run.assert_called_once_with(self.binary,self.output,'disabled',0,'http://127.0.0.1:1',False)
        self.assertEqual(len(results),1)

    def test_default_balanced_sequence_and_expiry_are_unchanged(self):
        for smoke,expected in [(False,['disabled','metadata_only','full_content','full_content','metadata_only','disabled','full_content']),(True,['disabled','metadata_only','full_content','full_content'])]:
            with self.subTest(smoke=smoke),patch.object(driver,'run_round',return_value={}) as run,patch.object(driver,'round_success',return_value=True):
                results=driver.run_sequence(self.binary,self.output,'http://127.0.0.1:1',smoke)
                self.assertEqual([call.args[2] for call in run.call_args_list],expected)
                self.assertEqual(len(results),len(expected))
                self.assertTrue(run.call_args_list[-1].args[-1])
                self.assertEqual(len(run.call_args_list[-1].args),7)

    def test_invalid_mode_is_rejected_before_owner_creation(self):
        with patch.object(driver,'run_round') as run:
            with self.assertRaises(ValueError):driver.run_sequence(self.binary,self.output,'http://127.0.0.1:1',False,'disabled-copy')
        run.assert_not_called()

    def test_plain_diagnostic_sequence_is_one_disabled_round(self):
        with patch.object(driver,'run_round',return_value={}) as run,patch.object(driver,'round_success',return_value=True):
            result=driver.run_sequence(self.binary,self.output,'http://127.0.0.1:1',False,'disabled',plain_diagnostic=True)
        run.assert_called_once_with(self.binary,self.output,'disabled',0,'http://127.0.0.1:1',False,plain_diagnostic=True)
        self.assertEqual(len(result),1)
        for mode,smoke in [(None,False),('full_content',False),('disabled',True)]:
            with patch.object(driver,'run_round') as run:
                with self.assertRaises(ValueError):driver.run_sequence(self.binary,self.output,'http://127.0.0.1:1',smoke,mode,plain_diagnostic=True)
                run.assert_not_called()

    def test_plain_diagnostic_preserves_warmup_load_and_original_pair(self):
        for plain,native in [(False,False),(True,False),(True,True)]:
            with self.subTest(plain=plain,native=native):
                root=self.output/(str(plain)+str(native));root.mkdir()
                owner=Mock(pid=123456);owner.poll.return_value=None;owner.wait.return_value=0
                connection=Mock();connection.getresponse.return_value.status=202
                original_mkdir=Path.mkdir
                def mkdir(path,*args,**kwargs):
                    original_mkdir(path,*args,**kwargs)
                    if path==root/'00-disabled':
                        (path/'ready.json').write_text(json.dumps({'url':'http://127.0.0.1:1'}))
                def window(*args):
                    value={'requests':3000}
                    driver.write(args[-1].with_suffix('.summary.json'),value)
                    return value
                with patch.object(Path,'mkdir',mkdir),patch.object(driver.subprocess,'Popen',return_value=owner) as spawn,patch.object(driver.http.client,'HTTPConnection',return_value=connection),patch.object(driver,'process_sample',self.metric),patch.object(driver,'request',return_value=self.sample()) as request,patch.object(driver,'window',side_effect=window) as run_window:
                    result=driver.run_round(self.binary,root,'disabled',0,'http://127.0.0.1:1',plain_diagnostic=plain,native_diagnostics=native)
                self.assertEqual(spawn.call_args.args[0], driver.content_performance_backend_command(self.binary,root/'00-disabled',native))
                self.assertEqual(spawn.call_args.kwargs['env']['UR_CONTENT_PERFORMANCE_NATIVE_DIAGNOSTICS'],'1' if native else '0')
                self.assertEqual(request.call_count,20)
                self.assertEqual([call.args[3] for call in run_window.call_args_list],[False] if plain else [False,True])
                self.assertTrue(all(call.args[4:6]==(200,15) for call in run_window.call_args_list))
                self.assertEqual(len(result['windows']),1 if plain else 2)
                self.assertEqual(result['window_scope'],'plain_only_diagnostic' if plain else 'original')
                owner.wait.assert_called_once()
                self.assertTrue(result['backend_reaped'])
                # Diagnostic mode does not turn missing financial evidence into success.
                self.assertFalse(driver.round_success(result))

    def test_native_diagnostics_is_explicit_and_preserves_default_command(self):
        expected=[str(self.binary),'-test.run=^TestContentHTTPPerformanceHarness$','-test.v','-test.timeout=10m']
        self.assertEqual(driver.content_performance_backend_command(self.binary,self.output),expected)
        self.assertEqual(driver.content_performance_backend_command(self.binary,self.output,True),expected+['-test.trace='+str(self.output/'runtime.trace')])
        with patch.object(driver,'run_round') as run:
            with self.assertRaises(ValueError):
                driver.run_sequence(self.binary,self.output,'http://127.0.0.1:1',False,'disabled',native_diagnostics=True)
            run.assert_not_called()
        with patch.object(driver,'run_round',return_value={}) as run,patch.object(driver,'round_success',return_value=True):
            driver.run_sequence(self.binary,self.output,'http://127.0.0.1:1',False,'disabled',plain_diagnostic=True,native_diagnostics=True)
        run.assert_called_once_with(self.binary,self.output,'disabled',0,'http://127.0.0.1:1',False,plain_diagnostic=True,native_diagnostics=True)

    @staticmethod
    def sample():
        now=time.monotonic_ns()
        return dict(marker='offline',status=200,operation_id='offline',latency_ms=1,arrival_to_complete_ms=1,scheduling_lag_ms=0,started_monotonic_ns=now,finished_monotonic_ns=now)

if __name__=='__main__':unittest.main()
