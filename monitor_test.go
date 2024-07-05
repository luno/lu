package lu

import (
	"testing"

	"github.com/luno/jettison/jtest"
)

func panicFN(err *error) {
	defer cleanPanic()(err)
	panic("Bang")
}

func Test_cleanPanic_Panic(t *testing.T) {
	var err error
	panicFN(&err)
	jtest.Require(t, errProcessPanicked, err)
}

func Test_cleanPanic_Normal(t *testing.T) {
	var err error
	cleanPanic()(&err)
	jtest.RequireNil(t, err)
}
