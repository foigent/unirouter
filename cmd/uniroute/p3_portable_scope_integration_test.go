//go:build integration

package main

import (
	"context"
	"errors"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// Match XI's producerScopeEnv/startProducer envelope. These values record an
// operator-approved disposable scope; they do not grant execution authority.
const p3PortableProducerSelector = "^TestSourceObservationsChatAndEmbeddingsAuthHistoryAndReplayIntegration$/^chat$/^legacy_shared$"

const p3PortableActions = "synthetic-db-redis,http,credentials,source-binding,resource-authorization,human-review-grant,secret-cleanup"

var p3PortableKeys = []string{"P3_PORTABLE", "P3_APPROVED_ROOT", "P3_APPROVED_RUN", "P3_APPROVED_ACTIONS", "P3_XI_DATABASE_PORT", "P3_UR_DATABASE_PORT", "P3_REDIS_PORT"}
var p3PortableConflicts = []string{
	"XI_P3_INTEROP", "XI_J1_INTEROP", "XI_CC14_INTEROP", "XI_CC15_INTEROP", "XI_NO_B_BROWSER", "XI_FORMAL_ACCOUNTS",
	"XINSIGHT_SCOPED_BROWSER", "XINSIGHT_PURPOSE_BROWSER", "XINSIGHT_A0_BROWSER_HARNESS", "XINSIGHT_A0_BROWSER_PAGING_FIXTURE", "XINSIGHT_A_MANAGED_BROWSER",
	"UR_J1_INTEROP", "UR_CC14_INTEROP", "UR_CC15_INTEROP",
}

type p3ProducerScope struct {
	root, run, credentialParent, evidenceParent string
	xiPort, urPort, redisPort                   uint16
	portable                                    bool
}

func p3ProducerScopeFromEnv(get func(string) string) (p3ProducerScope, error) {
	legacy := p3ProducerScope{credentialParent: p3CredentialParent, evidenceParent: p3EvidenceParent, xiPort: 56134, urPort: 56132, redisPort: 56379}
	present := false
	for _, key := range p3PortableKeys {
		present = present || get(key) != ""
	}
	if !present {
		return legacy, nil
	}
	deny := func() (p3ProducerScope, error) {
		return p3ProducerScope{}, errors.New("portable P3 requires an exact approved isolated producer scope")
	}
	if get("P3_PORTABLE") != "1" || get("UR_P3_INTEROP") != "1" || get("P3_APPROVED_ACTIONS") != p3PortableActions {
		return deny()
	}
	for _, key := range p3PortableConflicts {
		if get(key) != "" {
			return deny()
		}
	}
	scope := p3ProducerScope{root: get("P3_APPROVED_ROOT"), run: get("P3_APPROVED_RUN"), portable: true}
	if !regexp.MustCompile(`^p3-public-[a-z0-9][a-z0-9-]{0,63}$`).MatchString(scope.run) || !filepath.IsAbs(scope.root) || filepath.Clean(scope.root) != scope.root || filepath.Base(scope.root) != scope.run || len(strings.Split(strings.TrimPrefix(scope.root, "/"), "/")) < 3 {
		return deny()
	}
	ports := []*uint16{&scope.xiPort, &scope.urPort, &scope.redisPort}
	seen := map[uint64]bool{}
	for i, key := range []string{"P3_XI_DATABASE_PORT", "P3_UR_DATABASE_PORT", "P3_REDIS_PORT"} {
		value := get(key)
		n, err := strconv.ParseUint(value, 10, 16)
		if err != nil || n < 1024 || strconv.FormatUint(n, 10) != value || seen[n] {
			return deny()
		}
		seen[n] = true
		*ports[i] = uint16(n)
	}
	scope.credentialParent = filepath.Join(scope.root, "credentials/xi-product-test")
	scope.evidenceParent = filepath.Join(scope.root, "evidence")
	if get("P3_CREDENTIAL_DIR") != filepath.Join(scope.credentialParent, scope.run) || get("UR_P3_OUTPUT_DIR") != filepath.Join(scope.evidenceParent, scope.run, "ur") {
		return deny()
	}
	return scope, nil
}

func p3ProducerEntryMatches(name string, observations, content bool) bool {
	return name == "TestSourceObservationsChatAndEmbeddingsAuthHistoryAndReplayIntegration" && observations && !content
}

func p3ProducerInvocationMatches(name string, observations, content bool) bool {
	selector := flag.Lookup("test.run")
	return selector != nil && selector.Value.String() == p3PortableProducerSelector && p3ProducerEntryMatches(name, observations, content)
}

func p3ProducerDatabaseMatches(cfg *pgx.ConnConfig, port uint16, database string) bool {
	return cfg != nil && cfg.Host == "127.0.0.1" && cfg.Port == port && len(cfg.Fallbacks) == 0 && len(cfg.RuntimeParams) == 0 && cfg.Database == database && database != "" && database != "postgres" && !strings.HasPrefix(database, "template")
}

// This envelope is the portable XI producerScopeEnv plus startProducer values,
// without the parent XI/browser/formal-account flags or resource-rev2 admission.
func p3PortableProducerTestEnv(root, run string) map[string]string {
	return map[string]string{
		"P3_PORTABLE": "1", "UR_P3_INTEROP": "1", "P3_APPROVED_ROOT": root, "P3_APPROVED_RUN": run, "P3_APPROVED_ACTIONS": p3PortableActions,
		"P3_XI_DATABASE_PORT": "58134", "P3_UR_DATABASE_PORT": "58132", "P3_REDIS_PORT": "58379",
		"P3_CREDENTIAL_DIR": filepath.Join(root, "credentials/xi-product-test", run), "UR_P3_OUTPUT_DIR": filepath.Join(root, "evidence", run, "ur"),
		"REQUIRE_TEST_DATABASE": "1", "REDIS_DB": "15", "REDIS_ADDR": "127.0.0.1:58379",
		"TEST_DATABASE_URL":   "postgres://synthetic@127.0.0.1:58132/ur_p3_base?sslmode=disable",
		"UR_P3_BASE_DATABASE": "ur_p3_base", "UR_P3_DATABASE_OID": "12345", "UR_P3_SYSTEM_ID": "67890",
	}
}

func TestP3PortableProducerScope(t *testing.T) {
	get := func(env map[string]string) func(string) string { return func(key string) string { return env[key] } }
	legacy, err := p3ProducerScopeFromEnv(get(nil))
	if err != nil || legacy.portable || legacy.credentialParent != p3CredentialParent || legacy.evidenceParent != p3EvidenceParent || legacy.xiPort != 56134 || legacy.urPort != 56132 || legacy.redisPort != 56379 {
		t.Fatal("legacy defaults changed")
	}
	for _, mode := range []string{"UR_P3_INTEROP", "UR_J1_INTEROP", "UR_CC14_INTEROP", "UR_CC15_INTEROP"} {
		t.Run("legacy_"+mode, func(t *testing.T) {
			s, e := p3ProducerScopeFromEnv(get(map[string]string{mode: "1"}))
			if e != nil || s != legacy {
				t.Fatal("legacy scope changed")
			}
		})
	}
	run := "p3-public-contributor-20261011-r1"
	root := "/var/lib/foigent-validation/" + run
	valid := func() map[string]string { return p3PortableProducerTestEnv(root, run) }
	t.Run("XI_exact_child_envelope", func(t *testing.T) {
		scope, e := p3ProducerScopeFromEnv(get(valid()))
		if e != nil || !scope.portable || scope.root != root || scope.run != run || scope.urPort != 58132 || scope.xiPort != 58134 || scope.redisPort != 58379 {
			t.Fatal("XI child envelope rejected")
		}
	})
	for _, key := range append(append([]string{}, p3PortableKeys...), "UR_P3_INTEROP", "P3_CREDENTIAL_DIR", "UR_P3_OUTPUT_DIR") {
		t.Run("missing_"+key, func(t *testing.T) {
			env := valid()
			delete(env, key)
			if _, e := p3ProducerScopeFromEnv(get(env)); e == nil {
				t.Fatal("partial scope accepted")
			}
		})
	}
	for _, key := range p3PortableKeys {
		t.Run("partial_"+key, func(t *testing.T) {
			if _, e := p3ProducerScopeFromEnv(get(map[string]string{key: valid()[key]})); e == nil {
				t.Fatal("lone input accepted")
			}
		})
	}
	for _, key := range p3PortableConflicts {
		for _, value := range []string{"1", "0"} {
			t.Run("conflict_"+key+value, func(t *testing.T) {
				env := valid()
				env[key] = value
				if _, e := p3ProducerScopeFromEnv(get(env)); e == nil {
					t.Fatal("mixed mode accepted")
				}
			})
		}
	}
	for _, tc := range []struct{ key, value string }{
		{"P3_PORTABLE", "true"}, {"UR_P3_INTEROP", "0"}, {"P3_APPROVED_ACTIONS", "http"}, {"P3_APPROVED_ACTIONS", p3PortableActions + ",extra"},
		{"P3_APPROVED_RUN", j1RunID}, {"P3_APPROVED_RUN", "p3-public-../other"}, {"P3_APPROVED_RUN", "p3-public-Upper"}, {"P3_APPROVED_RUN", "p3-public-" + strings.Repeat("a", 65)},
		{"P3_APPROVED_ROOT", root + "/"}, {"P3_APPROVED_ROOT", "/" + run}, {"P3_APPROVED_ROOT", "relative/" + run}, {"P3_APPROVED_ROOT", "/var/lib/../validation/" + run},
		{"P3_APPROVED_ROOT", root + "-other"}, {"P3_CREDENTIAL_DIR", root + "/credentials/xi-product-test/previous"}, {"P3_CREDENTIAL_DIR", root + "/credentials/xi-product-test/../" + run}, {"UR_P3_OUTPUT_DIR", root + "/evidence/" + run + "/xi"},
		{"P3_XI_DATABASE_PORT", "0"}, {"P3_XI_DATABASE_PORT", "1023"}, {"P3_XI_DATABASE_PORT", "058134"}, {"P3_XI_DATABASE_PORT", "65536"}, {"P3_XI_DATABASE_PORT", "+58134"}, {"P3_XI_DATABASE_PORT", "127.0.0.1:58134"}, {"P3_UR_DATABASE_PORT", "58134"}, {"P3_REDIS_PORT", "58132"}, {"P3_REDIS_PORT", "58134"},
	} {
		t.Run("invalid_"+tc.key+"_"+tc.value, func(t *testing.T) {
			env := valid()
			env[tc.key] = tc.value
			if _, e := p3ProducerScopeFromEnv(get(env)); e == nil {
				t.Fatal("invalid scope accepted")
			}
		})
	}
}

func TestP3PortableProducerDatabaseBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, dsn, base string
		accepted        bool
	}{
		{"exact", "postgres://synthetic@127.0.0.1:58132/ur_p3_base?sslmode=disable", "ur_p3_base", true},
		{"XI_port", "postgres://synthetic@127.0.0.1:58134/ur_p3_base?sslmode=disable", "ur_p3_base", false},
		{"remote", "postgres://synthetic@db.example.invalid:58132/ur_p3_base?sslmode=disable", "ur_p3_base", false},
		{"localhost", "postgres://synthetic@localhost:58132/ur_p3_base?sslmode=disable", "ur_p3_base", false},
		{"hosts", "postgres://synthetic@127.0.0.1:58132,127.0.0.1:58135/ur_p3_base?sslmode=disable", "ur_p3_base", false},
		{"transport", "postgres://synthetic@127.0.0.1:58132/ur_p3_base?sslmode=prefer", "ur_p3_base", false},
		{"session", "postgres://synthetic@127.0.0.1:58132/ur_p3_base?sslmode=disable&search_path=other", "ur_p3_base", false},
		{"other_base", "postgres://synthetic@127.0.0.1:58132/other?sslmode=disable", "ur_p3_base", false},
		{"missing_base", "postgres://synthetic@127.0.0.1:58132/ur_p3_base?sslmode=disable", "", false},
		{"system", "postgres://synthetic@127.0.0.1:58132/postgres?sslmode=disable", "postgres", false},
		{"template", "postgres://synthetic@127.0.0.1:58132/template1?sslmode=disable", "template1", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, e := pgx.ParseConfig(tc.dsn)
			if e != nil {
				t.Fatal("inert configuration did not parse")
			}
			if p3ProducerDatabaseMatches(cfg, 58132, tc.base) != tc.accepted {
				t.Fatal("database boundary differs")
			}
		})
	}
	if p3ProducerDatabaseMatches(nil, 58132, "ur_p3_base") {
		t.Fatal("nil database accepted")
	}
}

func TestP3PortableProducerEntry(t *testing.T) {
	for _, tc := range []struct {
		name                            string
		observations, content, accepted bool
	}{
		{"TestSourceObservationsChatAndEmbeddingsAuthHistoryAndReplayIntegration", true, false, true},
		{"TestSourceObservationsChatAndEmbeddingsAuthHistoryAndReplayIntegration", false, false, false},
		{"TestSourceObservationsChatAndEmbeddingsAuthHistoryAndReplayIntegration", true, true, false},
		{"TestSourceFactsChatAndEmbeddingsAuthHistoryAndReplayIntegration", false, false, false},
		{"TestJ1ContentAuthenticatedPullIntegration", true, true, false},
		{"TestSourceObservationsChatAndEmbeddingsAuthHistoryAndReplayIntegration/chat/legacy_shared", true, false, false},
	} {
		if p3ProducerEntryMatches(tc.name, tc.observations, tc.content) != tc.accepted {
			t.Fatal("entry boundary differs")
		}
	}
}

func TestP3PortableProducerRefusesBeforeFixture(t *testing.T) {
	executable, e := os.Executable()
	if e != nil {
		t.Fatal("test executable unavailable")
	}
	const selector = p3PortableProducerSelector
	for _, tc := range []struct{ name, key, value, selector, diagnostic string }{
		{"partial", "P3_APPROVED_ACTIONS", "", selector, "approved portable producer scope"},
		{"parent_mode", "XI_P3_INTEROP", "1", selector, "approved portable producer scope"},
		{"J1_conflict", "UR_J1_INTEROP", "1", selector, "approved portable producer scope"},
		{"CC14_conflict", "UR_CC14_INTEROP", "1", selector, "approved portable producer scope"},
		{"duplicate_ports", "P3_UR_DATABASE_PORT", "58134", selector, "approved portable producer scope"},
		{"wrong_path", "UR_P3_OUTPUT_DIR", "/outside/ur", selector, "approved portable producer scope"},
		{"wrong_selector", "", "", "^TestSourceFactsChatAndEmbeddingsAuthHistoryAndReplayIntegration$/^chat$/^legacy_shared$", "portable P3 exact observation entry"},
		{"wrong_operation", "", "", "^TestSourceObservationsChatAndEmbeddingsAuthHistoryAndReplayIntegration$/^embeddings$/^legacy_shared$", "portable P3 exact observation entry"},
		{"wrong_identity", "", "", "^TestSourceObservationsChatAndEmbeddingsAuthHistoryAndReplayIntegration$/^chat$/^agent$", "portable P3 exact observation entry"},
		{"broad_selector", "", "", "^TestSourceObservationsChatAndEmbeddingsAuthHistoryAndReplayIntegration$", "portable P3 exact observation entry"},
		{"nonroot", "", "", selector, "root and required database guard"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "nonroot" && os.Geteuid() == 0 {
				t.Skip("nonroot refusal is checked only as an unprivileged user")
			}
			run := "p3-public-offline-refusal"
			root := filepath.Join(t.TempDir(), run)
			env := p3PortableProducerTestEnv(root, run)
			if tc.key != "" {
				env[tc.key] = tc.value
			}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, executable, "-test.run="+tc.selector, "-test.timeout=5s")
			cmd.Env = []string{}
			for key, value := range env {
				cmd.Env = append(cmd.Env, key+"="+value)
			}
			output, err := cmd.CombinedOutput()
			if err == nil || ctx.Err() != nil || !strings.Contains(string(output), tc.diagnostic) {
				t.Fatal("expected early refusal missing; raw child output withheld")
			}
			if _, err = os.Lstat(root); !os.IsNotExist(err) {
				t.Fatal("refused invocation created its scope root")
			}
		})
	}
}
