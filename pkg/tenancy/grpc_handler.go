package tenancy

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/cespare/xxhash"
	"github.com/golang-jwt/jwt/v4"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func (a *AuthExtension) NewGuardingUnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		// Handle case where tenant is in the context metadata
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, errors.New("missing metadata")
		}

		auths := md.Get(a.cfg.Header)
		if len(auths) == 0 {
			return nil, errors.New("missing authHeader")
		}

		tenantInfo, err := a.parseToken(auths[0])
		if err != nil {
			return nil, err
		}

		return handler(WithTenant(ctx, tenantInfo), req)
	}
}

func (a *AuthExtension) NewGuardingStreamInterceptor() grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		md, ok := metadata.FromIncomingContext(ss.Context())
		if !ok {
			return errors.New("missing metadata")
		}

		auths := md.Get(a.cfg.Header)
		if len(auths) == 0 {
			return errors.New("missing authHeader")
		}

		tenantInfo, err := a.parseToken(auths[0])
		if err != nil {
			return err
		}

		return handler(srv, steamInterceptorWrap{
			ServerStream: ss,
			context:      WithTenant(ss.Context(), tenantInfo),
		})
	}
}

type steamInterceptorWrap struct {
	grpc.ServerStream
	context context.Context
}

func (s steamInterceptorWrap) Context() context.Context {
	return s.context
}

type TenantInfo struct {
	TenantID  string `json:"tenant_id"`
	AccountID string `json:"account_id"`
}

func (a *AuthExtension) parseToken(tokenStr string) (*TenantInfo, error) {
	hash := xxhash.Sum64String(tokenStr)
	if val, ok := a.jwtCache.Load(hash); ok {
		if info, ok := val.(*TenantInfo); ok {
			// TODO check expired
			return info, nil
		}
	}

	parts := strings.SplitN(tokenStr, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return nil, errors.New("invalid authorization header format")
	}

	userInfo := parts[1]
	token, err := jwt.Parse(userInfo, func(token *jwt.Token) (interface{}, error) {
		if token.Method.Alg() != jwt.SigningMethodRS256.Alg() {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return a.publicKey, nil
	}, jwt.WithValidMethods([]string{"RS256"}))

	if err != nil || !token.Valid {
		return nil, fmt.Errorf("invalid JWT: %w", err)
	}

	var res TenantInfo
	if claims, ok := token.Claims.(jwt.MapClaims); ok {
		if tenant, ok := claims["tenant"].(map[string]any); ok {
			if tID, ok := tenant["tenant_id"].(string); ok {
				res.TenantID = tID
			}
			if aID, ok := tenant["account_id"].(float64); ok {
				res.AccountID = strconv.FormatInt(int64(aID), 10)
			}
		}
	}
	a.jwtCache.Store(hash, token)
	return &res, nil
}

func WithTenant(ctx context.Context, tenant *TenantInfo) context.Context {
	ctx = context.WithValue(ctx, tenantKey, tenant)
	return ctx
}
