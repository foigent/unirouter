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
