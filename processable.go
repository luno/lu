package lu

import "context"

type Processable interface {
	func() | func(ctx context.Context) | func() error
}

// WrapProcessFunc helper method to generate a ProcessFunc from
// an interface implementing the Processable methods
func WrapProcessFunc[P Processable](p P) ProcessFunc {
	var x any = p
	switch f := x.(type) {
	case func():
		return func(ctx context.Context) error {
			f()
			return nil
		}
	case func(ctx context.Context):
		return func(ctx context.Context) error {
			f(ctx)
			return nil
		}
	case func() error:
		return func(ctx context.Context) error {
			return f()
		}
	}
	panic("unreachable") // Should never be reached due to constraint
}
