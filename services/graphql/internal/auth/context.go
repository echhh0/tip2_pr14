package auth

import "context"

type contextKey string

const (
	authorizationKey contextKey = "authorization"
	requestIDKey     contextKey = "request_id"
)

func WithAuthorization(ctx context.Context, value string) context.Context {
	return context.WithValue(ctx, authorizationKey, value)
}

func AuthorizationFromContext(ctx context.Context) string {
	value, _ := ctx.Value(authorizationKey).(string)
	return value
}

func WithRequestID(ctx context.Context, value string) context.Context {
	return context.WithValue(ctx, requestIDKey, value)
}

func RequestIDFromContext(ctx context.Context) string {
	value, _ := ctx.Value(requestIDKey).(string)
	return value
}
