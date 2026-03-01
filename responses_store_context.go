package agentloop

import "context"

type responsesStoreContextKey struct{}

func WithResponsesStore(ctx context.Context, enabled bool) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, responsesStoreContextKey{}, enabled)
}

func ResponsesStoreFromContext(ctx context.Context) (bool, bool) {
	if ctx == nil {
		return false, false
	}
	v := ctx.Value(responsesStoreContextKey{})
	enabled, ok := v.(bool)
	if !ok {
		return false, false
	}
	return enabled, true
}
