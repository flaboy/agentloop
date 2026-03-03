package agentloop

import (
	"context"
	"net/http"
)

type AuthCredential struct {
	HeaderName  string
	HeaderValue string
}

type AuthProvider interface {
	ResolveAuth(ctx context.Context) (AuthCredential, error)
}

type EndpointProvider interface {
	ResolveEndpoint(ctx context.Context) (string, error)
}

type RequestMutator interface {
	MutateRequest(ctx context.Context, req *http.Request) error
}

type AuthProviderFunc func(ctx context.Context) (AuthCredential, error)

func (f AuthProviderFunc) ResolveAuth(ctx context.Context) (AuthCredential, error) {
	return f(ctx)
}

type EndpointProviderFunc func(ctx context.Context) (string, error)

func (f EndpointProviderFunc) ResolveEndpoint(ctx context.Context) (string, error) {
	return f(ctx)
}

type RequestMutatorFunc func(ctx context.Context, req *http.Request) error

func (f RequestMutatorFunc) MutateRequest(ctx context.Context, req *http.Request) error {
	return f(ctx, req)
}
