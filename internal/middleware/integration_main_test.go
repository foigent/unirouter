//go:build integration

package middleware

import (
	"testing"

	"github.com/uniroute/unirouter/internal/testutil"
)

func TestMain(m *testing.M) {
	testutil.RunWithMigratedTestDBTemplate(m)
}
