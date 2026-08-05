package resetexec

import (
	"os"
	"strings"
	"testing"
)

func TestResetExecutionContainsNoBroadDeletionOrSequenceBypassLiterals(t *testing.T) {
	payload, err := os.ReadFile("apply.go")
	if err != nil {
		t.Fatal(err)
	}
	source := strings.ToUpper(string(payload))
	for _, forbidden := range []string{
		"TRUN" + "CATE",
		"CAS" + "CADE",
		"SET" + "VAL(",
		"SESSION_" + "REPLICATION_ROLE",
		"DROP " + "TABLE",
		"DISABLE TRIGGER " + "ALL",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("reset execution contains forbidden literal %q", forbidden)
		}
	}
}
