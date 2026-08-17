//go:build integration

package modelgov

import (
	"testing"

	"github.com/uniroute/unirouter/internal/testutil"
)

func TestMain(m *testing.M) {
	testutil.RunWithMigratedTestDBTemplate(m)
}
