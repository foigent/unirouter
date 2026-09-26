//go:build integration

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

const cc15RunID = "cc15-openclaw-file-actions-20260925-r1"
const cc15Actions = "synthetic-db-redis,http,credentials,guardrail-shadow-policy,content-binding,full-content-policy,human-review-grant,secret-cleanup,openclaw-read-edit-read-five-rounds"
const cc15URL = "http://127.0.0.1:19285"
const cc15Upstream = "127.0.0.1:19286"
const cc15ReadCallID = "callcc15readinput"
const cc15ReportReadCallID = "callcc15readreport"
const cc15EditCallID = "callcc15editreport"
const cc15PostReadCallID = "callcc15readupdated"
const cc15ClientWorkspaceDir = "/private/tmp/ur-xi-data-actions-20260925/workspace"

func cc15EditSuccessText() string {
	return "Successfully replaced 1 block(s) in " + filepath.Join(cc15ClientWorkspaceDir, "report.md") + "."
}

const cc15Old = "完成记录：待填"
const cc15New = "完成记录：alpha 7，beta 11，合计 18"

const cc15InputBody = "# 合成项目资料\n\nalpha: 7\nbeta: 11\n\n仅供资料与修改结果工作链测试。"
const cc15ReportBefore = "# 合成项目周报\n\n完成记录：待填\n\n保留段落：本段不应被修改。"
const cc15ReportAfter = "# 合成项目周报\n\n完成记录：alpha 7，beta 11，合计 18\n\n保留段落：本段不应被修改。"

type cc15Schema struct {
	Type       string                     `json:"type"`
	Properties map[string]json.RawMessage `json:"properties"`
	Required   []string                   `json:"required"`
	Items      json.RawMessage            `json:"items"`
}
type cc15Tool struct {
	Type     string `json:"type"`
	Function struct {
		Name       string     `json:"name"`
		Parameters cc15Schema `json:"parameters"`
	} `json:"function"`
}
type cc15Request struct {
	cc14Request
	ActualTools []cc15Tool `json:"tools"`
}
type cc15Protocol struct {
	round    int
	readPath string
}

func cc15Property(schema cc15Schema, name, kind string) bool {
	var property cc15Schema
	return json.Unmarshal(schema.Properties[name], &property) == nil && property.Type == kind
}
func cc15RequiredOnly(schema cc15Schema, names ...string) bool {
	if len(schema.Required) != len(names) {
		return false
	}
	wanted := map[string]bool{}
	for _, name := range names {
		wanted[name] = true
	}
	for _, name := range schema.Required {
		if !wanted[name] {
			return false
		}
		delete(wanted, name)
	}
	return len(wanted) == 0
}
func cc15ToolResult(raw json.RawMessage) (string, error) {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, nil
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) != nil || len(blocks) == 0 {
		return "", errors.New("unsupported_tool_result")
	}
	var result strings.Builder
	for _, b := range blocks {
		if b.Type != "text" {
			return "", errors.New("unsupported_tool_result")
		}
		result.WriteString(b.Text)
		result.WriteByte('\n')
	}
	return result.String(), nil
}
func cc15ToolCall(name, id string, args any) (map[string]any, string, error) {
	encoded, err := json.Marshal(args)
	if err != nil {
		return nil, "", err
	}
	return map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": id, "type": "function", "function": map[string]any{"name": name, "arguments": string(encoded)}}}}, "tool_calls", nil
}
func (p *cc15Protocol) next(raw []byte) (map[string]any, string, error) {
	fail := func(code string) (map[string]any, string, error) { return nil, "", errors.New(code) }
	p.round++
	if p.round > 5 {
		return fail("extra_model_request")
	}
	if len(raw) > cc14MaxRequest {
		return fail("request_too_large")
	}
	var req cc15Request
	if json.Unmarshal(raw, &req) != nil || req.Model != "gpt-4o" || !req.Stream || !req.StreamOptions.IncludeUsage || req.MaxTokens != 1024 || len(req.Messages) == 0 {
		return fail("synthetic_request_contract")
	}
	var read, edit *cc15Tool
	for i := range req.ActualTools {
		switch req.ActualTools[i].Function.Name {
		case "read":
			if read != nil {
				return fail("duplicate_read_schema")
			}
			read = &req.ActualTools[i]
		case "edit":
			if edit != nil {
				return fail("duplicate_edit_schema")
			}
			edit = &req.ActualTools[i]
		default:
			return fail("unexpected_exposed_tool")
		}
	}
	if read == nil || edit == nil || read.Type != "function" || edit.Type != "function" || read.Function.Parameters.Type != "object" || edit.Function.Parameters.Type != "object" {
		return fail("actual_read_edit_schemas_required")
	}
	readSchema := read.Function.Parameters
	path := ""
	for _, name := range []string{"path", "file_path"} {
		if cc15Property(readSchema, name, "string") {
			path = name
			break
		}
	}
	if path == "" || !cc15RequiredOnly(readSchema, path) {
		return fail("unsupported_read_schema")
	}
	editSchema := edit.Function.Parameters
	if !cc15Property(editSchema, "path", "string") || !cc15Property(editSchema, "edits", "array") || !cc15RequiredOnly(editSchema, "path", "edits") {
		return fail("unsupported_edit_schema")
	}
	var edits cc15Schema
	if json.Unmarshal(editSchema.Properties["edits"], &edits) != nil || json.Unmarshal(edits.Items, &edits) != nil || edits.Type != "object" || !cc15Property(edits, "oldText", "string") || !cc15Property(edits, "newText", "string") || !cc15RequiredOnly(edits, "oldText", "newText") {
		return fail("unsupported_edit_replacement_schema")
	}
	if p.round == 1 {
		for _, m := range req.Messages {
			if m.Role == "tool" || len(m.ToolCalls) != 0 || bytes.Contains(m.Content, []byte("# 合成项目资料")) {
				return fail("unexpected_initial_tool_history")
			}
		}
		p.readPath = path
		return cc15ToolCall("read", cc15ReadCallID, map[string]string{path: "input.md"})
	}
	if path != p.readPath {
		return fail("read_schema_changed")
	}
	calls, results := 0, 0
	inputResult, reportResult, editResult, postResult := "", "", "", ""
	for _, m := range req.Messages {
		for _, call := range m.ToolCalls {
			calls++
			if m.Role != "assistant" || call.Type != "function" {
				return fail("tool_call_history_mismatch")
			}
			switch calls {
			case 1, 2, 4:
				var args map[string]string
				wantID, wantPath := cc15ReadCallID, "input.md"
				if calls == 2 {
					wantID, wantPath = cc15ReportReadCallID, "report.md"
				}
				if calls == 4 {
					wantID, wantPath = cc15PostReadCallID, "report.md"
				}
				if call.ID != wantID || call.Function.Name != "read" || json.Unmarshal([]byte(call.Function.Arguments), &args) != nil || len(args) != 1 || args[path] != wantPath {
					return fail("read_call_history_mismatch")
				}
			case 3:
				var args struct {
					Path  string `json:"path"`
					Edits []struct {
						OldText string `json:"oldText"`
						NewText string `json:"newText"`
					} `json:"edits"`
				}
				if call.ID != cc15EditCallID || call.Function.Name != "edit" || json.Unmarshal([]byte(call.Function.Arguments), &args) != nil || args.Path != "report.md" || len(args.Edits) != 1 || args.Edits[0].OldText != cc15Old || args.Edits[0].NewText != cc15New {
					return fail("edit_call_history_mismatch")
				}
			default:
				return fail("extra_tool_call")
			}
		}
		if m.Role == "tool" {
			results++
			if results != calls || results > 4 {
				return fail("tool_result_order_mismatch")
			}
			result, err := cc15ToolResult(m.Content)
			if err != nil {
				return fail("unsupported_tool_result")
			}
			switch results {
			case 1:
				if m.ToolCallID != cc15ReadCallID {
					return fail("input_result_identity_mismatch")
				}
				inputResult = result
			case 2:
				if m.ToolCallID != cc15ReportReadCallID {
					return fail("report_result_identity_mismatch")
				}
				reportResult = result
			case 3:
				if m.ToolCallID != cc15EditCallID {
					return fail("edit_result_identity_mismatch")
				}
				editResult = result
			case 4:
				if m.ToolCallID != cc15PostReadCallID {
					return fail("post_read_result_identity_mismatch")
				}
				postResult = result
			}
		}
	}
	if p.round == 2 {
		if calls != 1 || results != 1 {
			return fail("exactly_one_input_read_roundtrip_required")
		}
		if strings.TrimSpace(inputResult) != cc15InputBody {
			return fail("actual_input_result_required")
		}
		return cc15ToolCall("read", cc15ReportReadCallID, map[string]string{path: "report.md"})
	}
	if p.round == 3 {
		if calls != 2 || results != 2 || strings.TrimSpace(inputResult) != cc15InputBody || strings.TrimSpace(reportResult) != cc15ReportBefore {
			return fail("actual_report_result_required")
		}
		return cc15ToolCall("edit", cc15EditCallID, map[string]any{"path": "report.md", "edits": []any{map[string]string{"oldText": cc15Old, "newText": cc15New}}})
	}
	if p.round == 4 {
		if calls != 3 || results != 3 || strings.TrimSpace(inputResult) != cc15InputBody || strings.TrimSpace(reportResult) != cc15ReportBefore || strings.TrimSpace(editResult) != cc15EditSuccessText() {
			return fail("actual_edit_result_required")
		}
		return cc15ToolCall("read", cc15PostReadCallID, map[string]string{path: "report.md"})
	}
	if calls != 4 || results != 4 || strings.TrimSpace(inputResult) != cc15InputBody || strings.TrimSpace(reportResult) != cc15ReportBefore || strings.TrimSpace(editResult) != cc15EditSuccessText() || strings.TrimSpace(postResult) != cc15ReportAfter {
		return fail("actual_post_edit_read_required")
	}
	return map[string]any{"role": "assistant", "content": "已读取 input.md 与 report.md，并把完成记录更新为 alpha 7、beta 11、合计 18；保留段落未改。"}, "stop", nil
}

// Pure guards: no database, network, credential, OpenClaw invocation or file write.
func cc15PureRequest(t *testing.T, stage int) []byte {
	t.Helper()
	messages := []any{map[string]any{"role": "user", "content": "读取 input.md 和 report.md，修改 report.md。"}}
	if stage >= 2 {
		messages = append(messages, map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"id": cc15ReadCallID, "type": "function", "function": map[string]any{"name": "read", "arguments": `{"path":"input.md"}`}}}}, map[string]any{"role": "tool", "tool_call_id": cc15ReadCallID, "content": "# 合成项目资料\n\nalpha: 7\nbeta: 11\n\n仅供资料与修改结果工作链测试。\n"})
	}
	if stage >= 3 {
		messages = append(messages, map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"id": cc15ReportReadCallID, "type": "function", "function": map[string]any{"name": "read", "arguments": `{"path":"report.md"}`}}}}, map[string]any{"role": "tool", "tool_call_id": cc15ReportReadCallID, "content": "# 合成项目周报\n\n完成记录：待填\n\n保留段落：本段不应被修改。\n"})
	}
	if stage >= 4 {
		messages = append(messages, map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"id": cc15EditCallID, "type": "function", "function": map[string]any{"name": "edit", "arguments": `{"path":"report.md","edits":[{"oldText":"完成记录：待填","newText":"完成记录：alpha 7，beta 11，合计 18"}]}`}}}}, map[string]any{"role": "tool", "tool_call_id": cc15EditCallID, "content": cc15EditSuccessText()})
	}
	if stage >= 5 {
		messages = append(messages, map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"id": cc15PostReadCallID, "type": "function", "function": map[string]any{"name": "read", "arguments": `{"path":"report.md"}`}}}}, map[string]any{"role": "tool", "tool_call_id": cc15PostReadCallID, "content": "# 合成项目周报\n\n完成记录：alpha 7，beta 11，合计 18\n\n保留段落：本段不应被修改。\n"})
	}
	tools := []any{map[string]any{"type": "function", "function": map[string]any{"name": "read", "parameters": map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}, "required": []string{"path"}}}}, map[string]any{"type": "function", "function": map[string]any{"name": "edit", "parameters": map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}, "edits": map[string]any{"type": "array", "items": map[string]any{"type": "object", "properties": map[string]any{"oldText": map[string]any{"type": "string"}, "newText": map[string]any{"type": "string"}}, "required": []string{"oldText", "newText"}}}}, "required": []string{"path", "edits"}}}}}
	wire, err := json.Marshal(map[string]any{"model": "gpt-4o", "stream": true, "stream_options": map[string]bool{"include_usage": true}, "max_tokens": 1024, "messages": messages, "tools": tools})
	if err != nil {
		t.Fatal(err)
	}
	return wire
}
func TestCC15FileActionProtocolGuardPure(t *testing.T) {
	first, second, third, fourth, fifth := cc15PureRequest(t, 1), cc15PureRequest(t, 2), cc15PureRequest(t, 3), cc15PureRequest(t, 4), cc15PureRequest(t, 5)
	p := &cc15Protocol{}
	for i, wire := range [][]byte{first, second, third, fourth, fifth} {
		delta, finish, err := p.next(wire)
		if err != nil {
			t.Fatalf("stage %d: %v", i+1, err)
		}
		if i < 4 && finish != "tool_calls" || i == 4 && (finish != "stop" || delta["content"] == "") {
			t.Fatalf("stage %d response", i+1)
		}
	}
	if _, _, err := p.next(fifth); err == nil || err.Error() != "extra_model_request" {
		t.Fatal("extra request accepted")
	}
	for _, tc := range []struct {
		name   string
		stage  int
		mutate func([]byte) []byte
	}{
		{"missing-edit-schema", 1, func(b []byte) []byte { return bytes.ReplaceAll(b, []byte(`"name":"edit"`), []byte(`"name":"other"`)) }},
		{"wrong-edit-field", 1, func(b []byte) []byte { return bytes.ReplaceAll(b, []byte(`"oldText"`), []byte(`"old_text"`)) }},
		{"false-input-result", 2, func(b []byte) []byte { return bytes.ReplaceAll(b, []byte("alpha: 7"), []byte("alpha: 70")) }},
		{"wrong-input-id", 2, func(b []byte) []byte {
			return bytes.ReplaceAll(b, []byte(`"tool_call_id":"`+cc15ReadCallID+`"`), []byte(`"tool_call_id":"other"`))
		}},
		{"false-report-result", 3, func(b []byte) []byte { return bytes.ReplaceAll(b, []byte(cc15Old), []byte("假冒旧值")) }},
		{"wrong-report-id", 3, func(b []byte) []byte {
			return bytes.ReplaceAll(b, []byte(`"tool_call_id":"`+cc15ReportReadCallID+`"`), []byte(`"tool_call_id":"other"`))
		}},
		{"wrong-edit-target", 4, func(b []byte) []byte { return bytes.ReplaceAll(b, []byte("report.md"), []byte("other.md")) }},
		{"false-edit-result", 4, func(b []byte) []byte {
			return bytes.ReplaceAll(b, []byte("Successfully replaced"), []byte("No changes made"))
		}},
		{"relative-edit-result", 4, func(b []byte) []byte {
			return bytes.Replace(b, []byte(cc15EditSuccessText()), []byte("Successfully replaced 1 block(s) in report.md."), 1)
		}},
		{"other-absolute-edit-target", 4, func(b []byte) []byte {
			return bytes.Replace(b, []byte(cc15EditSuccessText()), []byte("Successfully replaced 1 block(s) in /private/tmp/other/workspace/report.md."), 1)
		}},
		{"redacted-edit-result-in-raw-provider-request", 4, func(b []byte) []byte {
			return bytes.Replace(b, []byte(cc15EditSuccessText()), []byte("Successfully replaced 1 block(s) in $WORKSPACE_DIR/report.md."), 1)
		}},
		{"wrong-edit-id", 4, func(b []byte) []byte {
			return bytes.ReplaceAll(b, []byte(`"tool_call_id":"`+cc15EditCallID+`"`), []byte(`"tool_call_id":"other"`))
		}},
		{"false-post-read", 5, func(b []byte) []byte { return bytes.ReplaceAll(b, []byte(cc15New), []byte("完成记录：伪造")) }},
		{"missing-post-readback", 5, func(b []byte) []byte {
			var wire map[string]any
			if json.Unmarshal(b, &wire) != nil {
				return b
			}
			messages := wire["messages"].([]any)
			messages[len(messages)-1].(map[string]any)["content"] = ""
			changed, _ := json.Marshal(wire)
			return changed
		}},
		{"wrong-post-read-id", 5, func(b []byte) []byte {
			return bytes.ReplaceAll(b, []byte(`"tool_call_id":"`+cc15PostReadCallID+`"`), []byte(`"tool_call_id":"other"`))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &cc15Protocol{}
			if tc.stage >= 2 {
				if _, _, err := p.next(first); err != nil {
					t.Fatal(err)
				}
			}
			if tc.stage >= 3 {
				if _, _, err := p.next(second); err != nil {
					t.Fatal(err)
				}
			}
			if tc.stage >= 4 {
				if _, _, err := p.next(third); err != nil {
					t.Fatal(err)
				}
			}
			if tc.stage >= 5 {
				if _, _, err := p.next(fourth); err != nil {
					t.Fatal(err)
				}
			}
			input := [][]byte{first, second, third, fourth, fifth}[tc.stage-1]
			if _, _, err := p.next(tc.mutate(input)); err == nil {
				t.Fatal("invalid history/schema accepted")
			}
		})
	}
}

func TestCC15ApprovalGuardPure(t *testing.T) {
	env := map[string]string{
		"UR_CC15_INTEROP": "1", "CC15_APPROVED_RUN": cc15RunID, "CC15_APPROVED_ACTIONS": cc15Actions,
		"P3_CREDENTIAL_DIR": filepath.Join(p3CredentialParent, cc15RunID), "UR_P3_OUTPUT_DIR": filepath.Join(p3EvidenceParent, cc15RunID, "ur"),
		"REQUIRE_TEST_DATABASE": "1", "REDIS_ADDR": "127.0.0.1:56379", "REDIS_DB": "15",
		"TEST_DATABASE_URL":   "postgres://127.0.0.1:56132/cc15_synthetic_base?sslmode=disable",
		"UR_P3_BASE_DATABASE": "cc15_synthetic_base", "UR_P3_DATABASE_OID": "12345", "UR_P3_SYSTEM_ID": "67890",
		"CC15_APPROVED_BINARY_SHA256": strings.Repeat("a", 64),
	}
	get := func(key string) string { return env[key] }
	if err := cc14ApprovalError(get); err != nil {
		t.Fatal("exact inert CC15 admission rejected")
	}
	for _, tc := range []struct{ key, value string }{
		{"UR_CC14_INTEROP", "1"}, {"UR_CC15_INTEROP", ""},
		{"CC14_APPROVED_BINARY_SHA256", strings.Repeat("a", 64)},
		{"CC15_APPROVED_RUN", cc15RunID + "-other"}, {"CC15_APPROVED_ACTIONS", cc14Actions},
		{"P3_CREDENTIAL_DIR", filepath.Join(p3CredentialParent, cc14RunID)},
		{"UR_P3_OUTPUT_DIR", filepath.Join(p3EvidenceParent, cc14RunID, "ur")},
		{"REDIS_DB", "0"}, {"UR_P3_BASE_DATABASE", "postgres"},
		{"CC15_APPROVED_BINARY_SHA256", strings.Repeat("A", 64)},
	} {
		t.Run(tc.key, func(t *testing.T) {
			old := env[tc.key]
			env[tc.key] = tc.value
			defer func() { env[tc.key] = old }()
			if err := cc14ApprovalError(get); err == nil {
				t.Fatal("unsafe admission")
			}
		})
	}
}
