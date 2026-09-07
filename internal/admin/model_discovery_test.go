package admin

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestModelDiscoveryOnlyUsesExactWorkspaceEndpoint(t *testing.T) {
	valid := modelDiscoverySnapshot{Provider: "dashscope", AdapterType: "openai_compatible", BaseURL: "https://llm-test123.cn-beijing.maas.aliyuncs.com/compatible-mode/v1"}
	target, err := modelDiscoveryURL(valid, 2, "qwen & glm")
	require.NoError(t, err)
	u, err := url.Parse(target)
	require.NoError(t, err)
	require.Equal(t, "/api/v1/models", u.Path)
	require.Equal(t, "qwen & glm", u.Query().Get("name"))
	require.Equal(t, "2", u.Query().Get("page_no"))
	for _, raw := range []string{"http://llm-test123.cn-beijing.maas.aliyuncs.com/compatible-mode/v1", "https://127.0.0.1/compatible-mode/v1", "https://llm-test123.cn-beijing.maas.aliyuncs.com.evil.test/compatible-mode/v1", "https://dashscope.aliyuncs.com/compatible-mode/v1", "https://llm-test123.cn-beijing.maas.aliyuncs.com:443/compatible-mode/v1", "https://user@llm-test123.cn-beijing.maas.aliyuncs.com/compatible-mode/v1", "https://llm-test123.cn-beijing.maas.aliyuncs.com/compatible-mode/v1?q=x", "https://llm-test123.cn-beijing.maas.aliyuncs.com/apps/anthropic"} {
		s := valid
		s.BaseURL = raw
		_, err := modelDiscoveryURL(s, 1, "")
		require.Error(t, err, raw)
	}
}

const discoveryFixture = `{"success":true,"output":{"page_no":1,"page_size":50,"total":2,"models":[{"model":"qwen3.7-flash","name":"Qwen Flash","provider":"qwen","features":["function-calling"],"prices":[{"price":"0"}],"model_info":{"context_window":1000000000}},{"model":"new-unsupported-model","name":"Unknown"}]}}`

func TestModelDiscoveryKeepsMetadataSeparateFromExecution(t *testing.T) {
	r, err := decodeModelDiscovery([]byte(discoveryFixture), 1)
	require.NoError(t, err)
	require.Len(t, r.Models, 2)
	require.Equal(t, []string{"function-calling"}, r.Models[0].Features)
	body, err := json.Marshal(r)
	require.NoError(t, err)
	for _, term := range []string{"prices", "context_window", "qualified", "supportsTools", "executable", "api_key"} {
		require.NotContains(t, string(body), term)
	}
	for _, raw := range []string{
		strings.Replace(discoveryFixture, `"success":true`, `"success":true,"success":false`, 1),
		strings.Replace(discoveryFixture, `"page_no":1`, `"page_no":2`, 1),
		strings.Replace(discoveryFixture, `"new-unsupported-model"`, `"qwen3.7-flash"`, 1),
		strings.Replace(discoveryFixture, `"qwen3.7-flash"`, `" qwen3.7-flash"`, 1),
		`{"success":true,"output":{"page_no":1,"page_size":50,"models":null}}`,
	} {
		_, err := decodeModelDiscovery([]byte(raw), 1)
		require.Error(t, err)
	}
}
