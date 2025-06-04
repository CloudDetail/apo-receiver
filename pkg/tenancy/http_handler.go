package tenancy

import (
	"net/http"

	"github.com/kataras/iris/v12/context"
)

func (a *AuthExtension) AuthMiddleware() context.Handler {
	return func(ctx *context.Context) {
		header := ctx.Request().Header.Get(a.cfg.Header)
		if len(header) == 0 {
			ctx.StatusCode(http.StatusUnauthorized)
			ctx.WriteString("missing tenant header")
			return
		}

		tenant, err := a.parseToken(header)
		if err != nil {
			ctx.StatusCode(http.StatusUnauthorized)
			ctx.WriteString(err.Error())
			return
		}

		ctx.Values().Set(string(accountIDKey), tenant.AccountID)
		ctx.Values().Set(string(tenantIDKey), tenant.TenantID)

		ctx.Next()
	}
}
