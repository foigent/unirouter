package admin

import (
	"context"
	"errors"
	"testing"

	"github.com/uniroute/unirouter/internal/testutil"
)

func TestValidatePublicModelIdentitiesRejectsDisplayTitlesBeforeDatabaseLookup(t *testing.T) {
	for _, value := range []string{"Kimi K2.7 Code", "DeepSeek-V4-Flash", "model/with/path"} {
		if err := validatePublicModelIdentities(context.Background(), &testutil.MockDB{}, 7, value); !errors.Is(err, errInvalidPublicModelID) {
			t.Fatalf("model %q error = %v, want invalid public model ID", value, err)
		}
	}
}
