package tenancy

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"sync"

	"github.com/CloudDetail/apo-receiver/pkg/config"
)

type tenantCtxKey string

const (
	// tenantIDKey  = tenantCtxKey("__tenant_id__")
	// accountIDKey = tenantCtxKey("__account_id__")

	tenantKey = tenantCtxKey("__tenant__")
)

var emptyTenant = TenantInfo{
	TenantID:  "__empty__",
	AccountID: "-1",
}

var systemTenant = TenantInfo{
	TenantID:  "__SYSTEM__",
	AccountID: "multitenant",
}

type AuthExtension struct {
	cfg *config.TenancyConfig

	jwtCache sync.Map

	publicKey *rsa.PublicKey
}

func NewAuthExtension(cfg *config.TenancyConfig) (*AuthExtension, error) {
	if cfg == nil || !cfg.Enabled {
		return nil, nil
	}

	if len(cfg.Header) == 0 {
		cfg.Header = "Authorization"
	}
	if len(cfg.Scheme) == 0 {
		cfg.Scheme = "Bearer"
	}

	publicKey, err := parsePublicKey(cfg.PublicKey)
	if err != nil {
		panic(err)
	}

	return &AuthExtension{
		cfg:       cfg,
		publicKey: publicKey,
	}, nil
}

func parsePublicKey(keyStr string) (*rsa.PublicKey, error) {
	decoded, err := base64.StdEncoding.DecodeString(keyStr)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(decoded)
	if block == nil || block.Type != "PUBLIC KEY" {
		return nil, err
	}
	pubKey, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}

	publicKey, ok := pubKey.(*rsa.PublicKey)
	if ok {
		return publicKey, nil
	}
	return nil, fmt.Errorf("expected *rsa.PublicKey, got %T", pubKey)
}

func GetTenantID(ctx context.Context) string {
	val := ctx.Value(tenantKey)
	if val == nil {
		return ""
	}
	if tenant, ok := val.(*TenantInfo); ok {
		return tenant.TenantID
	}
	return ""
}

func GetTenant(ctx context.Context) TenantInfo {
	val := ctx.Value(tenantKey)
	if val == nil {
		return TenantInfo{
			TenantID:  "",
			AccountID: "",
		}
	}
	if tenant, ok := val.(*TenantInfo); ok {
		return *tenant
	}
	return TenantInfo{
		TenantID:  "",
		AccountID: "",
	}
}

func GetAccountID(ctx context.Context) string {
	val := ctx.Value(tenantKey)
	if val == nil {
		return ""
	}
	if tenant, ok := val.(*TenantInfo); ok {
		return tenant.AccountID
	}
	return ""
}

func SystemCtx() context.Context {
	return WithTenant(context.Background(), &systemTenant)
}
