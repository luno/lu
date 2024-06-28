package lu

import (
	"context"
	"testing"

	"github.com/luno/jettison/errors"
	"github.com/stretchr/testify/require"
)

func TestFuncMakeProcessFunc(t *testing.T) {
	ctx := context.Background()
	called := false
	f := func() { called = true }
	p := WrapProcessFunc(f)
	require.NotNil(t, p)
	err := p(ctx)
	require.Nil(t, err)
	require.True(t, called)
}

func TestCtxFuncMakeProcessFunc(t *testing.T) {
	ctx := context.Background()
	called := false
	call := &called
	f := func(_ context.Context) { *call = true }
	p := WrapProcessFunc(f)
	require.NotNil(t, p)
	err := p(ctx)
	require.Nil(t, err)
	require.True(t, called)
}

func TestErrorFuncMakeProcessFunc(t *testing.T) {
	ctx := context.Background()
	called := false
	call := &called
	f := func() error { *call = true; return errors.New("dummy") }
	p := WrapProcessFunc(f)
	require.NotNil(t, p)
	err := p(ctx)
	require.NotNil(t, err)
	require.True(t, called)
}
