//go:build integration

package admin

import (
	"fmt"
	"os"
	"testing"

	"github.com/uniroute/unirouter/internal/testutil"
)

func TestMain(m *testing.M) {
	testutil.EnableMigratedTestDBTemplate()
	code := m.Run()
	if err := testutil.CleanupMigratedTestDBTemplate(); err != nil {
		fmt.Fprintf(os.Stderr, "cleanup migrated test database template: %v\n", err)
		code = 1
	}
	os.Exit(code)
}
