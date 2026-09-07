package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
)

// Optional integration with the installed client, never a copied SDK rule.
// Set OPENCLAW_TRANSPORT_MODULE to that version's exported transports.mjs.
// This invokes only its pure payload builder with synthetic data and no key.
func TestInstalledOpenClawTransportConsumesProjectedDefaults(t *testing.T) {
	module := os.Getenv("OPENCLAW_TRANSPORT_MODULE")
	if module == "" {
		t.Skip("installed OpenClaw transport not selected")
	}
	require.NotEmpty(t, os.Getenv("OPENCLAW_RUNTIME_HOST_MODULE"), "select the same installed version's runtime host, not inert SDK defaults")
	node := os.Getenv("OPENCLAW_NODE_BINARY")
	if node == "" {
		node = "node"
	}
	models, _, err := projectOpenClawModels("unirouter", []catalogModel{{ID: "fixture", Execution: catalogExecutionProfile{
		Status: "verified", Source: "stable_release", ContextWindow: 204800,
		MaxOutputTokens: 131072, RequestDefaultOutputTokens: 65536, Capabilities: []string{"tools"},
	}}}, nil)
	require.NoError(t, err)
	input, err := json.Marshal(models[0])
	require.NoError(t, err)
	command := exec.Command(node, "--input-type=module", "-e", `
import assert from 'node:assert/strict';
import fs from 'node:fs';
import {pathToFileURL} from 'node:url';
import dc from 'node:diagnostics_channel';
globalThis.fetch = () => { throw Error('network forbidden'); };
dc.channel('undici:request:create').subscribe(() => process.exit(90));
await import(pathToFileURL(process.env.OPENCLAW_RUNTIME_HOST_MODULE));
const {buildOpenAICompletionsParams: build} = await import(pathToFileURL(process.env.OPENCLAW_TRANSPORT_MODULE));
const projected = JSON.parse(fs.readFileSync(0,'utf8'));
const model = {...projected, provider:'unirouter', api:'openai-completions', baseUrl:'https://gateway.invalid/v1',
  compat:{...projected.compat,maxTokensField:'max_completion_tokens'}};
const context = {messages:[{role:'user',content:'Read the fixture.',timestamp:0}],
  tools:[{name:'read',description:'Read a fixture',parameters:{type:'object',properties:{path:{type:'string'}}}}]};
const defaults = build(model,context,{});
assert.equal(model.maxTokens,131072);
assert.equal(defaults.max_completion_tokens,65536);
assert.equal(defaults.stream,true);
assert.equal(defaults.tool_choice,'auto');
assert.equal(build(model,context,{maxTokens:8192}).max_completion_tokens,8192);
assert.equal(build(model,context,{maxTokens:204800}).max_completion_tokens,131072);
const retained = {...model,maxTokens:8192,params:{maxTokens:4096}};
assert.equal(build(retained,context,{}).max_completion_tokens,4096);
const shortContext = {...model,contextWindow:8200,maxTokens:8192,params:{maxTokens:8192}};
const bounded = build(shortContext,context,{}).max_completion_tokens;
assert.ok(bounded > 0 && bounded < 8192);
process.stdout.write('installed payload defaults, overrides, ceiling and context bounds passed; provider requests=0\n');
`)
	command.Stdin = bytes.NewReader(input)
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
	t.Log(string(output))
}

// Exercise the installed host's menu and selected transport, including its
// onPayload hook. No request is allowed past the last pre-send hook.
func TestInstalledOpenClawThinkingControlsReachActualTransport(t *testing.T) {
	if os.Getenv("OPENCLAW_TRANSPORT_MODULE") == "" {
		t.Skip("installed OpenClaw transport not selected")
	}
	require.NotEmpty(t, os.Getenv("OPENCLAW_RUNTIME_HOST_MODULE"))
	require.NotEmpty(t, os.Getenv("OPENCLAW_THINKING_MODULE"))
	require.NotEmpty(t, os.Getenv("OPENCLAW_EXTRA_PARAMS_MODULE"))
	var fixtures []map[string]any
	for _, item := range []struct{ id, contract string }{
		{"qwen3.7-flash", "reasoning.qwen3.7-hybrid.v1"},
		{"qwen3.8-max", "reasoning.qwen3.8-max-hybrid.v2"},
		{"deepseek-v4-pro", "reasoning.deepseek-v4-pro-thinking-toggle.v5"},
		{"deepseek-v4-flash", "reasoning.deepseek-v4-flash-thinking-toggle.v5"},
		{"custom-stable-alias", "reasoning.deepseek-v4-pro-thinking-toggle.v5"},
		{"glm-5.2", "reasoning.glm-5.2-thinking-toggle.v1"},
		{"glm-5.3", "reasoning.glm-5.3-required.v1"},
		{"minimax-m2.7", "reasoning.minimax-m27-required-embedded.v2"},
		{"glm-5.1", "reasoning.glm-5-family-thinking-toggle.v1"},
		{"kimi-k3", "reasoning.kimi-k3-effort.v2"},
	} {
		fixture := reasoningFixture(item.contract)
		fixture.ID = item.id
		models, report, err := projectOpenClawModels("unirouter", []catalogModel{fixture}, nil)
		require.NoError(t, err)
		fixtures = append(fixtures, map[string]any{"model": models[0], "projection": report.Controls[0]})
	}
	input, err := json.Marshal(fixtures)
	require.NoError(t, err)
	node := os.Getenv("OPENCLAW_NODE_BINARY")
	if node == "" {
		node = "node"
	}
	command := exec.Command(node, "--input-type=module", "-e", `
import assert from 'node:assert/strict';
import fs from 'node:fs';
import dc from 'node:diagnostics_channel';
import {pathToFileURL} from 'node:url';
let requests = 0, captures = 0;
globalThis.fetch = () => { requests++; throw Error('network forbidden'); };
dc.channel('undici:request:create').subscribe(() => process.exit(90));
await import(pathToFileURL(process.env.OPENCLAW_RUNTIME_HOST_MODULE));
const {createOpenAICompletionsTransportStreamFn} = await import(pathToFileURL(process.env.OPENCLAW_TRANSPORT_MODULE));
const {a:listLevels,s:resolveDefault,o:resolveSupported} = await import(pathToFileURL(process.env.OPENCLAW_THINKING_MODULE));
const {t:applyExtraParamsToAgent} = await import(pathToFileURL(process.env.OPENCLAW_EXTRA_PARAMS_MODULE));
const streamFn = createOpenAICompletionsTransportStreamFn();
const fixtures = JSON.parse(fs.readFileSync(0,'utf8'));
const user = {role:'user',content:'Read the fixture.',timestamp:0};
const assistant = {role:'assistant',api:'openai-completions',provider:'unirouter',model:'fixture',
  content:[{type:'thinking',thinking:'Need the fixture.',thinkingSignature:'reasoning_content'},{type:'toolCall',id:'call_1',name:'read',arguments:{path:'fixture'}}],
  stopReason:'toolUse',timestamp:1,usage:{input:1,output:1,totalTokens:2,cost:{input:0,output:0,total:0}}};
const tool = {role:'toolResult',toolCallId:'call_1',toolName:'read',content:[{type:'text',text:'fixture says yes'}],isError:false,timestamp:2};
for (const {model:raw,projection} of fixtures) {
  const model = {...raw,provider:'unirouter',api:'openai-completions',baseUrl:'https://gateway.invalid/v1',
    compat:{...raw.compat,maxTokensField:'max_completion_tokens'}};
  const levels = listLevels(model.provider,model.id,[model],'openclaw');
  assert.deepEqual(levels,projection.client_levels ?? [],model.id+' host menu');
  assert.ok(!levels.includes('max') && !levels.includes('ultra'));
  const defaultLevel = resolveDefault({provider:model.provider,model:model.id,catalog:[model],agentRuntime:'openclaw'});
  for (const selection of [undefined,...levels]) {
    const level = selection ?? defaultLevel;
    if (levels.length) assert.equal(resolveSupported({provider:model.provider,model:model.id,catalog:[model],agentRuntime:'openclaw',level}),level);
    for (const continuation of [false,true]) {
      let payload;
      const context = {messages:continuation?[user,{...assistant,model:model.id},tool]:[user],
        tools:[{name:'read',description:'Read fixture',parameters:{type:'object',properties:{path:{type:'string'}}}}]};
      const agent = {streamFn};
      applyExtraParamsToAgent(agent,{},'unirouter',model.id,undefined,level,undefined,undefined,model,undefined,undefined,{preparedExtraParams:{}});
      const stream = agent.streamFn(model,context,{apiKey:'synthetic-test-only',reasoningEffort:level==='off'?'none':level,
        onPayload:p=>{ payload=p; captures++; throw Error('captured-before-send'); }});
      for await (const event of stream) { assert.notEqual(event.type,'done'); }
      assert.ok(payload,model.id+' onPayload');
      assert.equal(payload.stream,true);
      assert.equal(payload.tool_choice,'auto');
      assert.equal(payload.max_completion_tokens,8192);
      assert.equal(payload.thinking_budget,undefined);
      const nativeWrapper = ['deepseek-v4-flash','deepseek-v4-pro'].includes(model.id);
      if (nativeWrapper) {
        assert.ok(['off','high'].includes(level));
        assert.deepEqual(payload.thinking,{type:level==='off'?'disabled':'enabled'});
        assert.equal(payload.reasoning_effort,level==='off'?undefined:'high');
      } else if (projection.mode==='toggle') {
        assert.equal(payload.thinking,undefined);
        assert.equal(payload.enable_thinking,level!=='off');
        assert.equal(payload.reasoning_effort,undefined);
      } else if (projection.mode==='effort') {
        assert.equal(payload.enable_thinking,undefined);
        assert.equal(payload.reasoning_effort,level==='off'?'none':projection.client_level_mapping[level],model.id+' '+level);
      } else {
        assert.equal(payload.enable_thinking,undefined);
        assert.equal(payload.reasoning_effort,undefined);
      }
      if (continuation) {
        assert.ok(payload.messages.some(m=>m.role==='tool' && m.tool_call_id==='call_1'));
        assert.ok(payload.messages.some(m=>m.role==='assistant' && m.tool_calls?.[0]?.id==='call_1'));
        const previous = payload.messages.find(m=>m.role==='assistant' && m.tool_calls?.[0]?.id==='call_1');
        if (nativeWrapper && level==='off') assert.equal(previous.reasoning_content,undefined,'native non-thinking wrapper omits reasoning replay');
        else assert.equal(previous.reasoning_content,'Need the fixture.',model.id+' '+level+' must preserve reasoning history through the normal wrapper');
      }
    }
  }
  process.stdout.write(JSON.stringify({model:model.id,levels,clientDefault:defaultLevel})+'\n');
}
assert.equal(requests,0);
process.stdout.write('actual transport captures='+captures+'; provider requests=0\n');
`)
	command.Stdin = bytes.NewReader(input)
	output, err := command.CombinedOutput()
	require.NoError(t, err, "%s", output)
	t.Log(string(output))
}
