package lu

import (
	"context"
	"os"
	"testing"

	"github.com/luno/jettison/jtest"
	"github.com/stretchr/testify/assert"
)

func TestPidFile(t *testing.T) {
	err := createPIDFile()
	jtest.RequireNil(t, err)

	contents, err := os.ReadFile(fileName)
	jtest.RequireNil(t, err)
	assert.NotEmpty(t, string(contents))

	removePIDFile(context.Background())

	_, err = os.ReadFile(fileName)
	jtest.Assert(t, os.ErrNotExist, err)
}
