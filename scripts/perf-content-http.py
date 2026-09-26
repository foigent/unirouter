#!/usr/bin/env python3
"""Opt-in Linux synthetic Chat HTTP measurement; no external provider calls.
Compile once with go test -c -tags=integration ./cmd/uniroute. Supply that binary,
new output directory and already-authorized isolated PG/Redis via environment.
The short smoke run verifies instrumentation and is NOT performance acceptance.
"""
import argparse, concurrent.futures, hashlib, http.client, http.server, json, math
import os, pathlib, statistics, subprocess, threading, time, urllib.parse

ANSWER = 's' * 8192
# Legacy stream admission uses a conservative decoded-byte upper bound.
OUTPUT_LIMIT = len(ANSWER.encode('utf-8'))
# Existing public source-HTTP fixture identity; never a customer/provider key.
FIXTURE_AUTH = 'Bearer ur-synthetic-source-' + 'a' * 32
provider_calls = {}
provider_lock = threading.Lock()

class Provider(http.server.BaseHTTPRequestHandler):
    protocol_version = 'HTTP/1.1'
    def log_message(self, *args):
        pass
    def do_POST(self):
        started = time.monotonic_ns()
        data = json.loads(self.rfile.read(int(self.headers['Content-Length'])))
        marker = data['messages'][0]['content'].split('|')[0]
        with provider_lock:
            entry = provider_calls.setdefault(marker, {'calls': 0})
            entry['calls'] += 1
            entry['max_tokens'] = data.get('max_tokens')
            entry['output_limit_valid'] = type(data.get('max_tokens')) is int and data['max_tokens'] == OUTPUT_LIMIT
            output_limit_valid = entry['output_limit_valid']
        if not output_limit_valid:
            self.send_error(400, f'Synthetic provider requires max_tokens={OUTPUT_LIMIT}')
            return
        base = {'id': marker, 'model': 'gpt-4o', 'created': int(time.time())}
        usage = {'prompt_tokens': 512, 'completion_tokens': 2048, 'total_tokens': 2560}
        if data.get('stream'):
            self.send_response(200)
            self.send_header('Content-Type', 'text/event-stream')
            self.send_header('Connection', 'close')
            self.end_headers()
            for offset in range(0, len(ANSWER), 1024):
                frame = dict(base, object='chat.completion.chunk', choices=[{'index': 0, 'delta': {'content': ANSWER[offset:offset+1024]}, 'finish_reason': None}])
                self.wfile.write(('data: ' + json.dumps(frame, separators=(',', ':')) + '\n\n').encode())
                self.wfile.flush()
            frame = dict(base, object='chat.completion.chunk', choices=[{'index': 0, 'delta': {}, 'finish_reason': 'stop'}])
            self.wfile.write(('data: ' + json.dumps(frame) + '\n\n').encode())
            frame = dict(base, object='chat.completion.chunk', choices=[], usage=usage)
            self.wfile.write(('data: ' + json.dumps(frame) + '\n\ndata: [DONE]\n\n').encode())
            self.wfile.flush()
            self.close_connection = True
        else:
            frame = dict(base, object='chat.completion', choices=[{'index': 0, 'message': {'role': 'assistant', 'content': ANSWER}, 'finish_reason': 'stop'}], usage=usage)
            encoded = json.dumps(frame, separators=(',', ':')).encode()
            self.send_response(200)
            self.send_header('Content-Type', 'application/json')
            self.send_header('Content-Length', str(len(encoded)))
            self.end_headers()
            self.wfile.write(encoded)
            self.wfile.flush()
        with provider_lock:
            provider_calls[marker]['service_ms'] = (time.monotonic_ns() - started) / 1e6


def write(path, value):
    with path.open('x') as f:
        json.dump(value, f, indent=2)


def request(endpoint, marker, stream, scheduled=None):
    payload = json.dumps({'model': 'company-chat', 'max_tokens': OUTPUT_LIMIT, 'messages': [{'role': 'user', 'content': marker + '|' + 'p'*2048}], 'stream': stream, **({'stream_options': {'include_usage': True}} if stream else {})}, separators=(',', ':')).encode()
    start = time.monotonic_ns()
    result = {'marker': marker, 'stream': stream, 'request_bytes': len(payload), 'scheduling_lag_ms': 0 if scheduled is None else max(0, (start-scheduled)/1e6)}
    conn = http.client.HTTPConnection(endpoint.hostname, endpoint.port, timeout=10)
    try:
        conn.request('POST', '/v1/chat/completions', payload, {'Authorization': FIXTURE_AUTH, 'Content-Type': 'application/json', 'Idempotency-Key': marker})
        response = conn.getresponse()
        result['status'] = response.status
        result['operation_id'] = response.getheader('X-Request-ID', '')
        if not result['operation_id']:
            result['error'] = 'missing_operation_id'
        if response.status != 200:
            body = response.read()
            result['response_bytes'] = len(body)
            result['error_response'] = body[:4096].decode('utf-8', errors='replace')
            result['error'] = 'http_status'
        elif stream:
            pieces, wire_bytes, done = [], 0, False
            event_data = []
            for line in response:
                wire_bytes += len(line)
                stripped = line.rstrip(b'\r\n')
                if stripped.startswith(b'data:'):
                    event_data.append(stripped[5:].lstrip(b' '))
                    continue
                if stripped or not event_data:
                    continue
                part = b'\n'.join(event_data)
                event_data = []
                if part == b'[DONE]':
                    done = True
                    continue
                event = json.loads(part)
                if 'error' in event:
                    result.setdefault('sse_errors', []).append(event['error'])
                    result['error'] = 'sse_error'
                for choice in event.get('choices', []):
                    content = choice.get('delta', {}).get('content', '')
                    if content:
                        result.setdefault('first_content_ms', (time.monotonic_ns()-start)/1e6)
                        pieces.append(content)
            if event_data:
                result['error'] = 'incomplete_sse_event'
            result['response_bytes'] = wire_bytes
            if not done or ''.join(pieces) != ANSWER:
                result['error'] = 'sse_semantics'
        else:
            body = response.read()
            result['response_bytes'] = len(body)
            if json.loads(body)['choices'][0]['message']['content'] != ANSWER:
                result['error'] = 'response_semantics'
    except Exception as error:
        result['error'] = type(error).__name__
    finally:
        result['finished_monotonic_ns'] = time.monotonic_ns()
        result['started_monotonic_ns'] = start
        result['latency_ms'] = (result['finished_monotonic_ns']-start)/1e6
        result['arrival_to_complete_ms'] = result['latency_ms']+result['scheduling_lag_ms']
        if 'first_content_ms' in result:
            result['arrival_to_first_content_ms'] = result['first_content_ms']+result['scheduling_lag_ms']
        conn.close()
    return result


def percentile(values, p):
    if not values:
        return None
    ordered = sorted(values)
    return ordered[max(0, math.ceil(len(ordered)*p)-1)]


def process_sample(pid):
    status = pathlib.Path(f'/proc/{pid}/status').read_text()
    values = {}
    for line in status.splitlines():
        if line.startswith(('VmRSS:', 'VmHWM:')):
            key, value, _ = line.split()
            values[key[:-1]+'_bytes'] = int(value)*1024
    fields = pathlib.Path(f'/proc/{pid}/stat').read_text().split(') ', 1)[1].split()
    values['cpu_seconds'] = (int(fields[11])+int(fields[12]))/os.sysconf('SC_CLK_TCK')
    values['monotonic'] = time.monotonic()
    return values


def window(endpoint, pid, prefix, stream, rate, seconds, output):
    # Preserve offered arrivals and 256 workers. Only interruption cancels work
    # that has not started; it never launches replacement traffic.
    samples, resources, sampler_errors = [], [], []
    stop = threading.Event()
    def sample():
        try:
            while not stop.is_set():
                resources.append(process_sample(pid))
                stop.wait(.1)
        except Exception as error:
            sampler_errors.append(type(error).__name__)
    sampler = threading.Thread(target=sample, name='cc13-window-sampler')
    sampler.start()
    begin = time.monotonic_ns()
    futures, failure = [], None
    unfinished_at_interrupt = 0
    workers = None
    try:
        workers = concurrent.futures.ThreadPoolExecutor(max_workers=256)
        for i in range(rate*seconds):
            when = begin+int(i*1e9/rate)
            delay = (when-time.monotonic_ns())/1e9
            if delay > 0:
                time.sleep(delay)
            futures.append(workers.submit(request, endpoint, f'{prefix}-{i}', stream, when))
        for future in concurrent.futures.as_completed(futures):
            future.result()
    except BaseException as error:
        failure = error
        unfinished_at_interrupt = sum(not f.done() for f in futures)
    finally:
        try:
            if failure is not None:
                for future in futures:
                    future.cancel()
            if workers is not None:
                workers.shutdown(wait=True, cancel_futures=failure is not None)
        finally:
            stop.set()
            sampler.join()
        # Collect ALL completed futures, including those not yielded to the
        # interrupted as_completed consumer. Request() already records errors.
        future_errors = []
        for i, future in enumerate(futures):
            if future.cancelled():
                continue
            try:
                samples.append(future.result())
            except BaseException as error:
                future_errors.append({'marker': f'{prefix}-{i}', 'error': type(error).__name__})
        elapsed = (time.monotonic_ns()-begin)/1e9
        for result in samples:
            with provider_lock:
                result['provider'] = dict(provider_calls.get(result['marker'], {'calls': 0}))
        with output.with_suffix('.samples.jsonl').open('x') as f:
            for result in samples:
                f.write(json.dumps(result)+'\n')
        write(output.with_suffix('.resources.json'), resources)

        successes = [x for x in samples if x.get('status') == 200 and not x.get('error')]
        latencies = [x['latency_ms'] for x in successes]
        first = [x['first_content_ms'] for x in successes if 'first_content_ms' in x]
        duration = resources[-1]['monotonic']-resources[0]['monotonic'] if len(resources)>1 else 0
        cpu = (resources[-1]['cpu_seconds']-resources[0]['cpu_seconds'])/duration*100 if duration>0 else None
        summary = {'offered_rate': rate, 'offered_seconds': seconds, 'requests': len(samples), 'successes': len(successes), 'errors': len(samples)-len(successes), 'elapsed_until_all_responses_seconds': elapsed, 'completion_rate_including_drain': len(successes)/elapsed if elapsed else 0, 'client_worker_limit': 256, 'backend_sampled_peak_rss_bytes': max((x.get('VmRSS_bytes', 0) for x in resources), default=None), 'backend_lifetime_hwm_bytes': max((x.get('VmHWM_bytes', 0) for x in resources), default=None), 'backend_cpu_percent_one_core': cpu, 'backend_cpu_percent_four_cores': cpu/4 if cpu is not None else None, 'latency_ms': {str(p): percentile(latencies, p) for p in [.5,.95,.99]}, 'arrival_to_complete_ms': {str(p): percentile([x['arrival_to_complete_ms'] for x in successes],p) for p in [.5,.95,.99]}, 'arrival_to_first_content_ms': {str(p): percentile([x['arrival_to_first_content_ms'] for x in successes if 'arrival_to_first_content_ms' in x],p) for p in [.5,.95,.99]}, 'started_in_offered_window':sum(x['started_monotonic_ns']<=begin+seconds*1e9 for x in samples), 'completed_in_offered_window':sum(x['finished_monotonic_ns']<=begin+seconds*1e9 for x in successes), 'first_meaningful_sse_ms': {str(p): percentile(first,p) for p in [.5,.95,.99]}, 'scheduling_lag_ms_p95': percentile([x['scheduling_lag_ms'] for x in samples], .95), 'operation_ids_unique': len({x.get('operation_id') for x in samples})==len(samples) and all(x.get('operation_id') for x in samples), 'provider_call_mismatches': sum(x['provider']['calls'] != 1 for x in samples), 'latency_boundary': 'client call through full response; includes local provider and transport; successful samples reported alongside all failures'}
        summary.update(requested=rate*seconds, submitted=len(futures), completed=len(samples),
                       cancelled=sum(f.cancelled() for f in futures), unsubmitted=rate*seconds-len(futures),
                       unfinished_at_interrupt=unfinished_at_interrupt, unfinished_after_join=sum(not f.done() for f in futures), future_errors=future_errors,
                       interrupted=failure is not None, interruption_class=type(failure).__name__ if failure else None,
                       sampler_errors=sampler_errors)
        summary['classifications'] = classify_samples(samples)
        write(output.with_suffix('.summary.json'), summary)
    if failure is not None:
        raise failure
    return summary


def classify_samples(samples):
    counts = {}
    for sample in samples:
        code = None
        try:
            error = json.loads(sample.get('error_response', '{}')).get('error', {})
            if isinstance(error, dict):
                code = error.get('code') or error.get('type')
        except (ValueError, AttributeError):
            pass
        key = json.dumps({'http_status': sample.get('status'), 'error': sample.get('error'),
                          'code': code, 'provider_calls': sample.get('provider', {}).get('calls')}, sort_keys=True)
        counts[key] = counts.get(key, 0)+1
    return [dict(json.loads(key), count=value) for key, value in sorted(counts.items())]


def round_success(result):
    return (result.get('exit_code') == 0 and not result.get('error') and
            not result.get('cleanup_errors') and result.get('backend_reaped') is True and
            result.get('integrity') and all(result['integrity'].values()) and
            not result.get('warmup_errors') and
            all(not w['errors'] and not w['provider_call_mismatches'] and w['operation_ids_unique'] and
                not w.get('interrupted') and not w.get('sampler_errors') and not w.get('future_errors') and
                w.get('submitted') == w.get('requested') for w in result['windows']))

def lsn(value):
    high, low = value.split('/')
    return (int(high,16)<<32)+int(low,16)


def content_performance_backend_command(binary, output, native_diagnostics=False):
    command = [str(binary), '-test.run=^TestContentHTTPPerformanceHarness$', '-test.v', '-test.timeout=10m']
    if native_diagnostics:
        command.append('-test.trace='+str(output/'runtime.trace'))
    return command


def run_round(binary, root, mode, index, provider_url, smoke=False, expiry=False, plain_diagnostic=False, native_diagnostics=False):
    if native_diagnostics and not plain_diagnostic:
        raise ValueError('native diagnostics require the original plain diagnostic window')
    if plain_diagnostic and (mode != 'disabled' or smoke or expiry):
        raise ValueError('plain diagnostic requires disabled mode, full load and no expiry')
    output = root/f'{index:02d}-{mode}{"-expiry" if expiry else ""}'
    output.mkdir(mode=0o700)
    env = os.environ.copy()
    env.update(UR_CONTENT_PERFORMANCE='1',UR_CONTENT_PERFORMANCE_MODE=mode,UR_CONTENT_PERFORMANCE_OUTPUT=str(output),UR_CONTENT_PERFORMANCE_PROVIDER=provider_url,UR_CONTENT_PERFORMANCE_EXPIRY='1' if expiry else '0', UR_CONTENT_PERFORMANCE_NATIVE_DIAGNOSTICS='1' if native_diagnostics else '0')
    measured, warmup, lifecycle, cleanup_errors = [], [], [], []
    lifecycle_stop = threading.Event()
    lifecycle_sampler = None
    endpoint, backend, failure, code = None, None, None, None
    drain_start = None
    with (output/'backend.log').open('x') as log:
        try:
            backend = subprocess.Popen(content_performance_backend_command(binary, output, native_diagnostics),env=env,stdout=log,stderr=subprocess.STDOUT)
            ready_file = output/'ready.json'
            deadline = time.monotonic()+90
            while True:
                if backend.poll() is not None:
                    raise RuntimeError('backend_start_failed')
                if time.monotonic() > deadline:
                    raise TimeoutError('backend_readiness_timeout')
                try:
                    ready = json.loads(ready_file.read_text())
                    break
                except (FileNotFoundError, json.JSONDecodeError):
                    time.sleep(.1)
            endpoint = urllib.parse.urlparse(ready['url'])
            assert endpoint.scheme == 'http' and endpoint.hostname == '127.0.0.1'
            def sample_lifecycle():
                try:
                    while not lifecycle_stop.is_set():
                        try:
                            lifecycle.append(process_sample(backend.pid))
                        except FileNotFoundError:
                            if backend.poll() is None:
                                raise
                            break
                        lifecycle_stop.wait(.1)
                except Exception as error:
                    cleanup_errors.append({'step':'lifecycle_sampler','error':type(error).__name__})
            lifecycle_sampler = threading.Thread(target=sample_lifecycle, name='cc13-lifecycle-sampler')
            lifecycle_sampler.start()
            for i in range(10 if expiry else 20):
                warmup.append(request(endpoint,f'r{index}-warmup-{i}',False))
            if not expiry:
                rate,seconds=(2,2) if smoke else (200,15)
                for stream in ((False,) if plain_diagnostic else (False,True)):
                    name='stream' if stream else 'plain'
                    measured.append(window(endpoint,backend.pid,f'r{index}-{name}',stream,rate,seconds,output/name))
        except BaseException as error:
            failure = error
        finally:
            # Each cleanup is independent; neither finish nor evidence I/O may
            # skip wait/reap or leave non-daemon sampling threads alive.
            if endpoint is not None and endpoint.scheme == 'http' and endpoint.hostname == '127.0.0.1':
                conn = http.client.HTTPConnection(endpoint.hostname,endpoint.port,timeout=10)
                try:
                    conn.request('POST','/__fixture/finish')
                    response=conn.getresponse();response.read()
                    if response.status != 202:
                        raise RuntimeError('finish_not_accepted')
                except BaseException as error:
                    cleanup_errors.append({'step':'finish','error':type(error).__name__})
                finally:
                    conn.close()
            drain_start = time.monotonic()
            try:
                if backend is not None:
                    wait_deadline = drain_start+100
                    while True:
                        remaining = wait_deadline-time.monotonic()
                        if remaining <= 0:
                            raise subprocess.TimeoutExpired('fixture_owner',100)
                        try:
                            code = backend.wait(timeout=remaining)
                            break
                        except KeyboardInterrupt:
                            cleanup_errors.append({'step':'backend_wait_interrupt','error':'KeyboardInterrupt'})
                            # A signal during cleanup must not abandon the owner
                            # or reset the existing 100-second wait budget.
            except BaseException as error:
                cleanup_errors.append({'step':'backend_wait','error':type(error).__name__})
            finally:
                lifecycle_stop.set()
                if lifecycle_sampler is not None:
                    lifecycle_sampler.join()
            with provider_lock:
                for sample in warmup:
                    sample['provider'] = dict(provider_calls.get(sample['marker'], {'calls':0}))
            for name, value in [('warmup.json', warmup), ('lifecycle.resources.json', {
                    'samples':lifecycle, 'drain_start_monotonic':drain_start,
                    'boundary':'after readiness through load and owner exit; failure/timeout retained'})]:
                try:
                    write(output/name, value)
                except Exception as error:
                    cleanup_errors.append({'step':'write_'+name,'error':type(error).__name__})
    # Recover any interrupted window summary written before it raised.
    measured = [json.loads((output/(name+'.summary.json')).read_text())
                for name in ('plain','stream') if (output/(name+'.summary.json')).exists()]
    with provider_lock:
        for sample in warmup:
            sample['provider'] = dict(provider_calls.get(sample['marker'], {'calls':0}))
    if backend is not None and code is None:
        code = backend.poll()
    result={'mode':mode,'window_scope':'plain_only_diagnostic' if plain_diagnostic else 'original','exit_code':code,'windows':measured,'warmup_requests':len(warmup),
            'warmup_requested':10 if expiry else 20, 'warmup_uncompleted':(10 if expiry else 20)-len(warmup),
            'warmup_errors':sum(bool(x.get('error')) or x.get('status')!=200 for x in warmup),
            'warmup_classifications':classify_samples(warmup), 'cleanup_errors':cleanup_errors,
            'error':type(failure).__name__ if failure else None,
            'backend_pid':backend.pid if backend else None, 'backend_reaped':code is not None,
            'backend_alive':backend.poll() is None if backend else False}
    if native_diagnostics:
        result['native_diagnostics'] = {'trace_artifact':'runtime.trace', 'lifecycle_artifact':'workload-lifecycle.json',
                                        'trace_exists':(output/'runtime.trace').is_file(),
                                        'lifecycle_exists':(output/'workload-lifecycle.json').is_file(),
                                        'boundary':'diagnostic overhead; artifacts do not replace success gates'}
    for name in ('http-census','financial-before-wait','financial-after-wait','shutdown','policy-refresh'):
        if (output/(name+'.json')).exists():
            result[name] = {'artifact':name+'.json'}
    if (output/'after.json').exists():
        before=json.loads((output/'before.json').read_text());after=json.loads((output/'after.json').read_text())
        result['after']=after
        result['growth']={key:after[key]-before[key] for key in ['db_bytes','metadata_relation_bytes','manifest_count','manifest_document_bytes','stored_bytes','reserved_bytes','body_actual_bytes','body_allocated_bytes','body_files']}
        result['growth']['wal_bytes_including_background_and_warmup']=lsn(after['wal_lsn'])-lsn(before['wal_lsn'])
        expected=len(warmup)+sum(x['requests'] for x in measured)
        states=after['latest_capture_states']
        result['integrity']={'expected_requests':expected,'settled_all':after['settled_requests']==expected,'one_attempt_per_request':after['provider_attempts']==expected,'economic_all':after['economic_items']==expected,'four_observations_per_request':sum(states.values())==expected*4,'reserved_zero':after['reserved_bytes']==0,'outbox_empty':after['outbox_remaining']==0,'per_request_four_points':after['invalid_request_observation_sets']==0,'stored_matches_files':after['stored_bytes']==after['body_actual_bytes'],'object_states_exact':after['object_states']==({'ready':expected*4} if mode=='full_content' else {}),'mode_correct':states=={('complete/none' if mode=='full_content' else 'not_captured/policy_'+mode):expected*4},'full_body_complete':after['body_files']==(expected*4 if mode=='full_content' else 0)}
    write(output/'result.json',result)
    print(json.dumps({'round':index,'mode':mode,'exit_code':code,'error':result['error'],'cleanup_errors':cleanup_errors,'integrity':result.get('integrity'),'windows':measured}),flush=True)
    if failure is not None:
        raise failure
    return result

def run_sequence(binary, output, provider_url, smoke, selected_mode=None, plain_diagnostic=False, native_diagnostics=False):
    if native_diagnostics and not plain_diagnostic:
        raise ValueError('native diagnostics require --plain-diagnostic')
    if plain_diagnostic and (selected_mode != 'disabled' or smoke):
        raise ValueError('plain diagnostic requires --mode disabled without --smoke')
    if selected_mode is not None and selected_mode not in ('disabled','metadata_only','full_content'):
        raise ValueError('unsupported fixed mode')
    results=[]
    modes=['disabled','metadata_only','full_content'] if smoke else ['disabled','metadata_only','full_content','full_content','metadata_only','disabled']
    if selected_mode is not None:
        modes=[selected_mode]
    for i,mode in enumerate(modes):
        result=run_round(binary,output,mode,i,provider_url,smoke,**({'plain_diagnostic':True} if plain_diagnostic else {}), **({'native_diagnostics':True} if native_diagnostics else {}))
        results.append(result)
        if not round_success(result):
            return results
    if selected_mode is None:
        results.append(run_round(binary,output,'full_content',len(modes),provider_url,smoke,True))
    return results


def main():
    parser=argparse.ArgumentParser()
    parser.add_argument('--binary',type=pathlib.Path,required=True)
    parser.add_argument('--output',type=pathlib.Path,required=True)
    parser.add_argument('--smoke',action='store_true')
    parser.add_argument('--mode',choices=['disabled','metadata_only','full_content'],help='Run one fixed mode with the original window pair; omits the full comparison and expiry round')
    parser.add_argument('--plain-diagnostic',action='store_true',help='Only the original 200 RPS x 15 second plain window and 20 warmups; requires --mode disabled, excludes --smoke; not full CC13')
    parser.add_argument('--native-diagnostics',action='store_true',help='Opt-in decision/lifecycle evidence and native Go trace for --plain-diagnostic; original load and success gates remain unchanged')
    args=parser.parse_args()
    if args.native_diagnostics and not args.plain_diagnostic:
        parser.error('--native-diagnostics requires --plain-diagnostic')
    if args.plain_diagnostic and (args.mode != 'disabled' or args.smoke):
        parser.error('--plain-diagnostic requires --mode disabled without --smoke')
    args.binary=args.binary.resolve();args.output=args.output.resolve()
    assert pathlib.Path('/proc/self/status').exists(), 'Linux process accounting required'
    assert os.environ.get('REQUIRE_TEST_DATABASE')=='1'
    assert os.environ.get('REDIS_DB')=='15'
    args.output.mkdir(mode=0o700)
    identity={'binary_sha256':hashlib.sha256(args.binary.read_bytes()).hexdigest(),'driver_sha256':hashlib.sha256(pathlib.Path(__file__).read_bytes()).hexdigest(),'smoke_only':args.smoke,'selected_mode':args.mode,'mode_scope':'plain_only_diagnostic' if args.plain_diagnostic else ('single_mode_diagnostic' if args.mode else 'balanced_sequence'),'latency_gate_reference':'PRD1 P95 <500ms excluding upstream; this report retains conservative gross latency','cpu_reporting':'both one-core percent (docker convention) and normalized four-core percent; no invented threshold','boundary':'one public Chat HTTP synthetic path; not full CC13, installed artifact or production'}
    if args.native_diagnostics:
        identity['native_diagnostics'] = True
    write(args.output/'identity.json',identity)
    server=http.server.ThreadingHTTPServer(('127.0.0.1',0),Provider)
    provider=threading.Thread(target=server.serve_forever,daemon=True);provider.start()
    results=[]
    try:
        results=run_sequence(args.binary,args.output,f'http://127.0.0.1:{server.server_port}',args.smoke,args.mode,**({'plain_diagnostic':True} if args.plain_diagnostic else {}), **({'native_diagnostics':True} if args.native_diagnostics else {}))
    finally:
        server.shutdown();server.server_close();provider.join()
        # Round artifacts also cover interruption before run_round returns.
        results = [json.loads(p.read_text()) for p in sorted(args.output.glob('*/result.json'))]
        write(args.output/'results.json',results)
    success = len(results)==(1 if args.mode else (4 if args.smoke else 7)) and all(round_success(r) for r in results)
    # Successful instrumentation is distinct from the reported NFR values.
    return 0 if success else 1

if __name__=='__main__':
    raise SystemExit(main())
