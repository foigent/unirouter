package adapters

import "testing"

func TestResponseModelIdentityAcceptsOnlyExplicitSafeProviderIdentity(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "explicit", body: `{"model":"kimi-k2.7-code"}`, want: "kimi-k2.7-code"},
		{name: "trimmed", body: `{"model":"  deepseek-v4-flash  "}`, want: "deepseek-v4-flash"},
		{name: "missing", body: `{"choices":[]}`},
		{name: "nested is not authority", body: `{"result":{"model":"nested"}}`},
		{name: "whitespace is rejected", body: `{"model":"Kimi K2.7 Code"}`},
		{name: "control is rejected", body: "{\"model\":\"kimi-k3\\nforged\"}"},
		{name: "invalid json", body: `{`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := responseModelIdentity([]byte(test.body)); got != test.want {
				t.Fatalf("responseModelIdentity() = %q, want %q", got, test.want)
			}
		})
	}
}
