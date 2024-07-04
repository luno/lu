package process

import (
	"context"
	"testing"

	"github.com/luno/jettison/jtest"
	"github.com/stretchr/testify/require"
)

func Test_noopContextFunc(t *testing.T) {
	testcases := []struct {
		name string
		ctx  context.Context
	}{
		{
			name: "nil",
		},
		{
			name: "background",
			ctx:  context.Background(),
		},
		{
			name: "todo",
			ctx:  context.TODO(),
		},
	}
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cf, err := noOpContextFunc(tc.ctx)
			jtest.RequireNil(t, err)
			require.Equal(t, tc.ctx, ctx)
			require.NotNil(t, cf)
		})
	}
}
