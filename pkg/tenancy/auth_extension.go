package tenancy

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"strconv"
	"sync"

	"github.com/CloudDetail/apo-receiver/pkg/config"
)

type tenantCtxKey string

const (
	// tenantIDKey  = tenantCtxKey("__tenant_id__")
	// accountIDKey = tenantCtxKey("__account_id__")

	tenantKey = tenantCtxKey("__tenant__")

	userInfoHeader = "X-Userinfo"
)

var emptyTenant = UserInfo{
	Sub: "",
	Tenant: Tentant{
		TenantID:  "",
		AccountID: 0,
	},
}

var systemTenant = UserInfo{
	Sub: "",
	Tenant: Tentant{
		TenantID:    "__SYSTEM__",
		AccountID:   0,
		Multitenant: true,
	},
}

type AuthExtension struct {
	cfg *config.TenancyConfig

	jwtCache sync.Map

	publicKey *rsa.PublicKey

	base http.RoundTripper
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
		base:      http.DefaultTransport,
	}, nil
}

func (a *AuthExtension) RoundTrip(req *http.Request) (*http.Response, error) {
	req2 := req.Clone(req.Context())
	userInfo := GetTenant(req.Context())

	jsonBytes, err := json.Marshal(userInfo)
	if err != nil {
		return nil, err
	}

	encoded := base64.StdEncoding.EncodeToString(jsonBytes)
	req2.Header.Set(userInfoHeader, encoded)

	return a.base.RoundTrip(req2)
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
	if tenant, ok := val.(*UserInfo); ok {
		return tenant.Tenant.TenantID
	}
	return ""
}

func GetTenant(ctx context.Context) UserInfo {
	val := ctx.Value(tenantKey)
	if val == nil {
		return emptyTenant
	}
	if tenant, ok := val.(*UserInfo); ok {
		return *tenant
	}
	return emptyTenant
}

func GetAccountID(ctx context.Context) string {
	val := ctx.Value(tenantKey)
	if val == nil {
		return ""
	}
	if tenant, ok := val.(*UserInfo); ok {
		if tenant.Tenant.Multitenant {
			return "multitenant"
		}
		accountID := uint64(tenant.Tenant.AccountID)
		return strconv.FormatUint(accountID, 10)
	}
	return ""
}

func SystemCtx() context.Context {
	return WithTenant(context.Background(), &systemTenant)
}

func EmptyCtx() context.Context {
	return WithTenant(context.Background(), &emptyTenant)
}
