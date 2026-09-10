#!/usr/bin/env python3
"""Regenerate finite local-candidate schemas and independent synthetic vectors."""
from pathlib import Path
import re,json,hashlib,struct,copy
root=Path(__file__).resolve().parents[3];out=root/'contracts/source-facts/v2'
text=(root/'internal/sourcefacts/observation_contract.go').read_text()
def canonical(v):return json.dumps(v,sort_keys=True,separators=(',',':'),ensure_ascii=False).encode()
def scalar():return {'type':'string','minLength':1,'maxLength':256}
defs={}
for name,body in re.findall(r'type (\w+) struct \{(.*?)\n\}',text,re.S):
 props={};required=[]
 for typ,tag in re.findall(r'\w+\s+(\[\]\w+|\*?\w+)\s+`json:"([^"]+)"`',body):
  key=tag.split(',')[0]
  if typ.lstrip('*')=='string':s=scalar()
  elif typ=='bool':s={'type':'boolean'}
  elif typ=='[]string':s={'type':'array','items':scalar(),'maxItems':10,'uniqueItems':True}
  elif typ.startswith('[]'):s={'type':'array','items':{'$ref':'#/$defs/'+typ[2:]}}
  else:s={'$ref':'#/$defs/'+typ.lstrip('*')}
  props[key]=s
  if ',omitempty' not in tag:required.append(key)
 defs[name]={'type':'object','additionalProperties':False,'required':required,'properties':props}
rootdef=defs['Observation'];p=rootdef['properties']
profiles={
 'unirouter.runtime-observation.v1':['request_started','admission_rejected','request_terminal','delivery_started','local_delivery','output_held','request_context_done','response_measurement','first_output_measurement','stream_measurement','media_running','media_terminal','state_reconciled'],
 'unirouter.attempt-observation.v1':['attempt_registered','provider_response_received','provider_rejected','provider_outcome_unknown','provider_response_unusable','financial_result','delivery_started','local_delivery','output_held','round_trip_measurement','media_attempt_registered','media_provider_succeeded','media_retry_scheduled','media_result','state_reconciled'],
 'unirouter.capability-snapshot.v1':['selection'],
 'unirouter.policy-observation.v1':['input_result','output_result','stream_result'],
 'unirouter.destination-observation.v1':['dispatch_registered','dispatch_result']}
def enum(t,key,values):defs[t]['properties'][key]={'enum':values}
def pattern(t,key,pat,maxlen=256):defs[t]['properties'][key]={'type':'string','pattern':pat,'maxLength':maxlen}
for key in ['org_id','project_id','revision','previous_revision']:pattern('Observation',key,r'^[1-9][0-9]*$',19)
for key in ['source_fact_id','event_id','context_ref']:pattern('Observation',key,r'^uf1_[a-f0-9]{64}$',68)
for key in ['previous_hash','route_evidence_hash']:pattern('Observation',key,r'^sha256:[a-f0-9]{64}$',71)
for key in ['occurred_at','recorded_at']:pattern('Observation',key,r'^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{6}Z$',27)
enum('Observation','schema_version',['unirouter.source-observation.v1']);enum('Observation','coverage',['observed','partial','not_observed']);enum('Observation','profile',list(profiles));enum('Observation','subject_kind',['request','provider_attempt','media_job','media_attempt','policy'])
for key,maxlen in [('source_instance',128),('request_id',64),('subject_id',128),('correction_reason',128)]:p[key]['maxLength']=maxlen
provider=['not_started','registered','response_received','rejected','accepted','unknown','not_applicable'];delivery=['not_started','write_started','completed','write_failed','interrupted','unknown','not_applicable'];finance=['not_created','reserved','prepared','pending','settled','cancelled','unknown','not_applicable']
for t in ['RuntimeObservation','AttemptObservation']:
 enum(t,'provider',provider);enum(t,'local_delivery',delivery);enum(t,'financial',finance)
enum('RuntimeObservation','context_done_reason',['context_cancelled','context_deadline_exceeded'])
enum('RuntimeObservation','stream_mode',['streaming','non_streaming','not_observed']);enum('RuntimeObservation','admission',['authenticated','admitted','rejected','unknown']);enum('RuntimeObservation','capture',['observed','partial','not_observed'])
for key in ['route_sequence','route_total']:pattern('AttemptObservation',key,r'^[1-9][0-9]*$',19)
pattern('AttemptObservation','http_status',r'^[1-5][0-9]{2}$',3)
enum('CapabilityObservation','evidence_state',['available','partial','not_available']);pattern('CapabilityObservation','slot_version',r'^[1-9][0-9]*$',19);pattern('ProfileReference','sha256',r'^sha256:[a-f0-9]{64}$',71)
defs['CapabilityObservation']['properties']['required_capabilities']={'type':'array','maxItems':128,'uniqueItems':True,'items':{'type':'string','minLength':1,'maxLength':80}}
defs['CapabilityObservation']['properties']['capabilities']['maxItems']=64
pattern('CapabilityObservation','capability_evidence_hash',r'^sha256:[a-f0-9]{64}$',71)
pattern('CapabilityReference','evidence_ref_sha256',r'^sha256:[a-f0-9]{64}$',71)
pattern('CapabilityReference','revision',r'^[1-9][0-9]*$',19)
pattern('CapabilityReference','verified_at',r'^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{6}Z$',27)
enum('CapabilityReference','support_state',['enabled'])
defs['CapabilityReference']['properties']['capability_id']['maxLength']=80
defs['CapabilityReference']['properties']['evidence_state']['maxLength']=32
for key,values in {'stage':['input','output','stream'],'mode':['shadow','enforce'],'configured_action':['allow','audit','block','replace','mask','unknown'],'outcome':['allow','match','block','replace','error','not_covered','unknown'],'failure_mode':['open','closed','unknown'],'evidence_state':['observed','partial','not_observed']}.items():enum('PolicyObservation',key,values)
defs['PolicyObservation']['properties']['policy_ref']['maxLength']=160
for key,values in {'detector_outcomes':['pass','match','transform','indeterminate','unavailable','skipped'],'stage_decisions':['allow','block']}.items():
 defs['PolicyObservation']['properties'][key]={'type':'array','minItems':1,'maxItems':len(values),'uniqueItems':True,'items':{'enum':values}}
defs['PolicyObservation']['properties']['effects']={'type':'array','maxItems':6,'uniqueItems':True,'items':{'enum':['input_blocked','input_replaced','output_blocked','output_replaced','stream_terminated','partial_output']}}
defs['PolicyObservation']['allOf']=[{'if':{'properties':{'mode':{'const':'shadow'}}},'then':{'properties':{'effects':{'maxItems':0}}}}]
for key,values in {'region_source':['configured','not_available'],'dispatch':['registered','do_observed','response_observed','unknown'],'policy_evidence':['observed','not_observed','not_applicable'],'input_binding_state':['known','unknown','not_applicable']}.items():enum('DestinationObservation',key,values)
pattern('DestinationObservation','origin',r'^https?://(?:\[[0-9a-fA-F:]+\]|[^/@?#:\s]+)(?::[1-9][0-9]{0,4})?$',256)
boundaries={'request_response_complete':['trusted_ingress','protocol_response_written'],'stream_first_valid_output':['trusted_ingress','first_meaningful_frame_written_and_flushed'],'stream_complete':['trusted_ingress','protocol_terminal_written_and_flushed'],'attempt_http_round_trip':['http_do_called','http_do_returned']}
for key,values in {'kind':list(boundaries),'version':['1'],'unit':['nanoseconds'],'state':['observed','not_applicable','not_observed','legacy_unknown','invalid'],'resolution':['nanosecond_representation'],'source':['process_monotonic_clock']}.items():enum('Measurement',key,values)
pattern('Measurement','value',r'^(0|[1-9][0-9]*)$',19)
defs['Measurement']['oneOf']=[{'properties':{'kind':{'const':kind},'start_boundary':{'const':b[0]},'end_boundary':{'const':b[1]}}} for kind,b in boundaries.items()]
defs['Measurement']['allOf']=[{'if':{'properties':{'state':{'const':'observed'}}},'then':{'required':['value']},'else':{'not':{'required':['value']}}}]
payloads=['runtime','attempt','capability','policy','destination','measurement'];branches=[]
for (profile,kinds),payload in zip(profiles.items(),payloads[:5]):
 for kind in kinds:
  actual='measurement' if kind.endswith('_measurement') else payload
  branch={'properties':{'profile':{'const':profile},'observation_kind':{'const':kind},'capture_expected':{'type':'array','maxItems':10,'uniqueItems':True,'items':{'enum':kinds}},'subject_kind':{'enum':(['media_attempt'] if kind=='media_running' else ['request','media_job']) if payload=='runtime' else ['policy'] if payload=='policy' else ['provider_attempt','media_attempt']}},'required':[actual],'not':{'anyOf':[{'required':[x]} for x in payloads if x!=actual]}}
  if actual=='runtime':
   branch['properties']['runtime']={'allOf':[{'$ref':'#/$defs/RuntimeObservation'},({'required':['context_done_reason']} if kind=='request_context_done' else {'not':{'required':['context_done_reason']}})]}
  if actual=='measurement':
   kindmap={'response_measurement':'request_response_complete','first_output_measurement':'stream_first_valid_output','stream_measurement':'stream_complete','round_trip_measurement':'attempt_http_round_trip'}
   branch['properties']['measurement']={'allOf':[{'$ref':'#/$defs/Measurement'},{'properties':{'kind':{'const':kindmap[kind]}}}]}
  branches.append(branch)
rootdef['oneOf']=branches
rootdef['allOf']=[{'if':{'properties':{'revision':{'const':'1'}}},'then':{'not':{'anyOf':[{'required':[x]} for x in ['previous_revision','previous_hash','correction_reason']]}},'else':{'required':['previous_revision','previous_hash','correction_reason']}}]
schema={'$schema':'https://json-schema.org/draft/2020-12/schema','$id':'foigent://sources/unirouter/source-observation/v1','$ref':'#/$defs/Observation','$defs':defs}
(out/'source-observation.schema.json').write_bytes(json.dumps(schema,ensure_ascii=False,indent=2).encode()+b'\n')
def identity(parts):return 'uf1_'+hashlib.sha256(b''.join(struct.pack('>I',len(x.encode()))+x.encode() for x in parts)).hexdigest()
base={'schema_version':'unirouter.source-observation.v1','source_instance':'synthetic-ur','org_id':'2','project_id':'3','request_id':'request-1','profile':list(profiles)[0],'subject_kind':'request','subject_id':'request-1','observation_kind':'request_started','revision':'1','occurred_at':'2026-09-10T00:00:00.000000Z','recorded_at':'2026-09-10T00:00:00.000000Z','coverage':'observed','capture_expected':['request_started','request_terminal']}
valid=[]
def add(name,d):
 d['source_fact_id']=identity([d[x] for x in ['schema_version','source_instance','org_id','project_id','profile','request_id','subject_kind','subject_id','observation_kind']]);d['event_id']=identity([d['schema_version']+'.revision',d['source_fact_id'],d['revision']]);wire=canonical(d);valid.append({'name':name,'document':d,'canonical_utf8':wire.decode(),'source_sha256':'sha256:'+hashlib.sha256(wire).hexdigest()})
d=copy.deepcopy(base);d['runtime']={'operation':'openai.chat_completions.v1','stream_mode':'not_observed','admission':'authenticated','provider':'not_started','local_delivery':'not_started','financial':'not_created','capture':'observed'};add('request_started',d)
d=copy.deepcopy(base);d.update(profile=list(profiles)[1],subject_kind='provider_attempt',subject_id='attempt-1',observation_kind='attempt_registered',capture_expected=['attempt_registered','provider_response_received','provider_rejected','provider_outcome_unknown','provider_response_unusable']);d['attempt']={'attempt_id':'attempt-1','operation':'openai.chat_completions.v1','provider':'registered','local_delivery':'not_started','financial':'reserved','retry_authorized':False,'evidence_ref':'attempt-1'};add('attempt_registered',d)
d=copy.deepcopy(base);d.update(profile=list(profiles)[2],subject_kind='provider_attempt',subject_id='attempt-1',observation_kind='selection',capture_expected=['selection']);d['capability']={'model_ref':'synthetic-model','provider':'synthetic','operation':'openai.chat_completions.v1','surface':'openai.chat_completions','protocol':{'id':'integration.openai.chat.v1','version':'1.0.0'},'execution_profile':{'id':'unverified','version':'1.0.0'},'evidence_state':'partial','required_capabilities':['tools'],'capabilities':[{'capability_id':'tools','support_state':'enabled','evidence_state':'verified','evidence_ref_sha256':'sha256:'+hashlib.sha256(b'synthetic-evidence').hexdigest(),'verified_at':base['occurred_at'],'revision':'1'}]};add('selected_capability',d)
d=copy.deepcopy(base);d.update(profile=list(profiles)[3],subject_kind='policy',subject_id='policy-1',observation_kind='input_result',capture_expected=['input_result']);d['policy']={'detector_outcomes':['match'],'stage_decisions':['allow'],'policy_ref':'policy-1','policy_version':'1','detector_ref':'synthetic','detector_version':'1','stage':'input','mode':'shadow','configured_action':'block','outcome':'match','failure_mode':'closed','degraded':False,'mandatory':True,'effects':[],'evidence_state':'observed'};add('shadow_match_not_block',d)
d=copy.deepcopy(base);d.update(profile=list(profiles)[4],subject_kind='provider_attempt',subject_id='attempt-1',observation_kind='dispatch_registered',capture_expected=['dispatch_registered','dispatch_result']);d['destination']={'provider':'synthetic','origin':'https://provider.invalid','region_source':'not_available','dispatch':'registered','policy_evidence':'not_observed','input_binding_state':'unknown'};add('destination_registration_not_delivery',d)
for kind,b in boundaries.items():
 d=copy.deepcopy(base);obs={'request_response_complete':'response_measurement','stream_first_valid_output':'first_output_measurement','stream_complete':'stream_measurement','attempt_http_round_trip':'round_trip_measurement'}[kind];d.update(observation_kind=obs,capture_expected=[obs]);
 if kind=='attempt_http_round_trip':d.update(profile=list(profiles)[1],subject_kind='provider_attempt',subject_id='attempt-1')
 d['measurement']={'kind':kind,'version':'1','value':'0','unit':'nanoseconds','state':'observed','start_boundary':b[0],'end_boundary':b[1],'resolution':'nanosecond_representation','source':'process_monotonic_clock'};add(kind+'_true_zero',d)
invalid=[]
for name,modify in [('unknown_bearer',lambda d:d.update(bearer='synthetic-only')),('null_not_zero',lambda d:d['measurement'].update(value=None)),('false_first_output',lambda d:d['measurement'].update(end_boundary='headers_sent')),('unobserved_value',lambda d:d['measurement'].update(state='not_observed')),('wall_clock',lambda d:d['measurement'].update(source='wall_clock_subtraction'))]:
 d=copy.deepcopy(valid[-3]['document']);modify(d);invalid.append({'name':name,'document':d,'validation':'schema_and_domain'})
d=copy.deepcopy(valid[4]['document']);d['destination']['origin']='https://user:synthetic@provider.invalid/path?key=synthetic';invalid.append({'name':'raw_destination','document':d,'validation':'schema_and_domain'})
d=copy.deepcopy(valid[3]['document']);d['policy']['effects']=['input_blocked'];invalid.append({'name':'shadow_as_block','document':d,'validation':'schema_and_domain'})
d=copy.deepcopy(valid[1]['document']);d['source_fact_id']='uf1_'+'0'*64;invalid.append({'name':'wrong_fact_identity','document':d,'validation':'domain'})
d=copy.deepcopy(valid[0]['document']);d.update(observation_kind='request_context_done',capture_expected=['request_context_done']);d['runtime'].update(provider='unknown',local_delivery='unknown',financial='unknown',context_done_reason='context_cancelled');add('context_done_no_actor_inference',d)
bad=copy.deepcopy(d);bad['runtime']['context_done_reason']='employee_cancelled';invalid.append({'name':'unsupported_cancel_actor','document':bad,'validation':'schema_and_domain'})
for vector in invalid:vector['canonical_utf8']=canonical(vector['document']).decode()
(out/'golden-observations.json').write_text(json.dumps({'vector_version':'unirouter.source-observation-vectors.v1','fixture_only':True,'notes':'Independent Python length-prefixed identity and sorted JSON, not UR writer output. Alternatives, not a single publication history.','valid':valid,'invalid':invalid},ensure_ascii=False,indent=2)+'\n')
manifest={'schema_version':'unirouter.source-observations-manifest.v1','status':'local_candidate','connector_id':'unirouter.workfacts','connector_version':'2.0.0','source_product':'unirouter','protocol_version':'2.0.2','protocol_commit':'ad8fe7a38c9d7335657630baa8670dd07556b98c','profiles':list(profiles),'purpose':'xinsight.source.read','transport':'pull','bootstrap':'forward_only','max_items':50,'max_source_bytes':65536,'max_page_bytes_exclusive':8388608,'retention_seconds':604800,'history_backfill':False,'timeline_complete':False,'source_schema_file':'source-observation.schema.json','source_vectors_file':'golden-observations.json','source_schema_sha256':'sha256:'+hashlib.sha256((out/'source-observation.schema.json').read_bytes()).hexdigest()}
(out/'manifest.json').write_text(json.dumps(manifest,ensure_ascii=False,indent=2)+'\n')
print('Created schema, manifest and',len(valid),'positive /',len(invalid),'negative vectors')
# Current runtime has its own snapshot contract, never an event descriptor.
time_pattern=r'^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{6}Z$'
time_schema={'type':'string','pattern':time_pattern}
id_schema={'type':'string','pattern':r'^[1-9][0-9]*$','maxLength':19}
ref_schema={'type':'string','pattern':r'^uf1_[a-f0-9]{64}$'}
entry={'type':'object','additionalProperties':False,'required':['request_id','family','subject_kind','subject_id','operation','freshness','original_state','last_observed_at','evidence_ref'],'properties':{
'request_id':{'type':'string','minLength':1,'maxLength':64},'family':{'enum':['stream','media','request']},'subject_kind':{'enum':['provider_attempt','media_job','request']},'subject_id':{'type':'string','minLength':1,'maxLength':128},'operation':{'type':'string','minLength':1,'maxLength':128},'freshness':{'enum':['lease_valid_execution','leased_pending','last_known']},'original_state':{'type':'string','minLength':1,'maxLength':32},'last_observed_at':time_schema,'valid_until':time_schema,'lease_generation':id_schema,'evidence_ref':ref_schema,'checkpoint_ref':ref_schema},'allOf':[
{'if':{'properties':{'freshness':{'const':'last_known'}}},'then':{'not':{'anyOf':[{'required':['valid_until']},{'required':['lease_generation']},{'required':['checkpoint_ref']}] }},'else':{'required':['valid_until','lease_generation']}},
{'if':{'properties':{'freshness':{'const':'leased_pending'}}},'then':{'properties':{'family':{'const':'media'},'original_state':{'const':'pending'}}}}]}
schema={'$schema':'https://json-schema.org/draft/2020-12/schema','$id':'foigent://schemas/unirouter/runtime-current/v1','type':'object','additionalProperties':False,'required':['schema_version','source_instance','org_id','project_id','profile','observed_at','coverage','uncovered','entries','has_more','source_sha256'],'properties':{
'schema_version':{'const':'unirouter.runtime-current.v1'},'source_instance':{'type':'string','minLength':1,'maxLength':128},'org_id':id_schema,'project_id':id_schema,'profile':{'const':'unirouter.runtime-observation.v1'},'observed_at':time_schema,'coverage':{'enum':['lease_backed_inventory','requested_execution_snapshot']},'uncovered':{'type':'array','items':{'enum':['synchronous_requests_without_execution_lease','progress_between_lease_observations']},'uniqueItems':True,'minItems':2,'maxItems':2},'entries':{'type':'array','items':entry,'maxItems':50},'has_more':{'type':'boolean'},'next_cursor':{'type':'string','minLength':1,'maxLength':4096},'source_sha256':{'type':'string','pattern':r'^sha256:[a-f0-9]{64}$'}},'allOf':[{'if':{'properties':{'has_more':{'const':True}}},'then':{'required':['next_cursor']},'else':{'not':{'required':['next_cursor']}}}]}
(out/'runtime-current.schema.json').write_text(json.dumps(schema,indent=2)+'\n')
page={'schema_version':'unirouter.runtime-current.v1','source_instance':'synthetic-ur','org_id':'1','project_id':'2','profile':'unirouter.runtime-observation.v1','observed_at':'2026-09-10T00:00:00.000000Z','coverage':'lease_backed_inventory','uncovered':['synchronous_requests_without_execution_lease','progress_between_lease_observations'],'entries':[],'has_more':False}
runtime_valid=[]
def runtime_add(name,p):
 p=copy.deepcopy(p);wire=canonical(p);p['source_sha256']='sha256:'+hashlib.sha256(wire).hexdigest();runtime_valid.append({'name':name,'document':p,'canonical_source_utf8':wire.decode()})
runtime_add('empty_bounded_inventory',page)
active={'request_id':'cross-day','family':'stream','subject_kind':'provider_attempt','subject_id':'attempt-1','operation':'openai.chat_completions.v1','freshness':'lease_valid_execution','original_state':'active','last_observed_at':'2026-09-09T23:59:59.000000Z','valid_until':'2026-09-10T00:00:20.000000Z','lease_generation':'1','evidence_ref':'uf1_'+'1'*64,'checkpoint_ref':'uf1_'+'2'*64}
p=copy.deepcopy(page);p['entries']=[active];runtime_add('cross_day_execution_lease',p)
p=copy.deepcopy(page);p['coverage']='requested_execution_snapshot';p['entries']=[{'request_id':'sync','family':'request','subject_kind':'request','subject_id':'sync','operation':'openai.responses.v1','freshness':'last_known','original_state':'frozen','last_observed_at':'2026-09-09T23:59:59.000000Z','evidence_ref':'uf1_'+'3'*64}];runtime_add('sync_last_known_no_liveness',p)
runtime_invalid=[]
for name,field,value in [('last_known_lease','valid_until','2026-09-10T00:00:20.000000Z'),('raw_owner','lease_owner','private-owner'),('unknown_freshness','freshness','running')]:
 p=copy.deepcopy(runtime_valid[-1]['document']);p['entries'][0][field]=value;runtime_invalid.append({'name':name,'document':p})
p=copy.deepcopy(runtime_valid[1]['document']);del p['entries'][0]['lease_generation'];runtime_invalid.append({'name':'missing_generation','document':p})
p=copy.deepcopy(runtime_valid[0]['document']);p['has_more']=True;runtime_invalid.append({'name':'missing_continuation','document':p})
(out/'golden-runtime-current.json').write_text(json.dumps({'valid':runtime_valid,'invalid':runtime_invalid},indent=2)+'\n')
manifest=json.loads((out/'manifest.json').read_text());manifest['profile_transports']={p:['incremental_events'] for p in profiles};manifest['profile_transports']['unirouter.runtime-observation.v1'].append('current_snapshot')
manifest['runtime_current']={'path':'/api/v1/integrations/xinsight/source/v2/runtime/current','schema_file':'runtime-current.schema.json','schema_sha256':'sha256:'+hashlib.sha256((out/'runtime-current.schema.json').read_bytes()).hexdigest(),'vectors_file':'golden-runtime-current.json','pagination':'each_page_current_observation','coverage':'lease_backed_inventory','cache_control':'no-store'}
(out/'manifest.json').write_text(json.dumps(manifest,indent=2)+'\n')
