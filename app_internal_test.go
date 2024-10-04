package lu

import (
	"context"
	"testing"
)

func SetBackgroundContextForTesting(t *testing.T, ctx context.Context) {
	old := background
	t.Cleanup(func() { background = old })
	background = ctx
}
