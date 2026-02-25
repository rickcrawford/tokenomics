package events

import "context"

// Multi fans out events to multiple emitters.
type Multi struct {
	emitters []Emitter
}

// NewMulti creates a fan-out emitter that delivers to all given emitters.
func NewMulti(emitters ...Emitter) *Multi {
	return &Multi{emitters: emitters}
}

// Emit delivers the event to all registered emitters.
// Errors are logged but do not stop delivery to remaining emitters.
func (m *Multi) Emit(ctx context.Context, event Event) error {
	var firstErr error
	for _, e := range m.emitters {
		if err := e.Emit(ctx, event); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Close shuts down all registered emitters.
func (m *Multi) Close() error {
	var firstErr error
	for _, e := range m.emitters {
		if err := e.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Nop is a no-op emitter used when no webhooks are configured.
type Nop struct{}

func (Nop) Emit(context.Context, Event) error { return nil }
func (Nop) Close() error                      { return nil }
