package source

import "context"

type accessTokenContextKey struct{}

func WithAccessToken(ctx context.Context, accessToken string) context.Context {
	return context.WithValue(ctx, accessTokenContextKey{}, accessToken)
}

func AccessTokenFromContext(ctx context.Context) (string, bool) {
	value := ctx.Value(accessTokenContextKey{})
	token, ok := value.(string)
	if !ok || token == "" {
		return "", false
	}
	return token, true
}
