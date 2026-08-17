//go:build integration

package orgdirectory

import (
	"testing"

	"github.com/uniroute/unirouter/internal/testutil"
)

func TestMain(m *testing.M) {
	testutil.RunWithMigratedTestDBTemplate(m)
}
