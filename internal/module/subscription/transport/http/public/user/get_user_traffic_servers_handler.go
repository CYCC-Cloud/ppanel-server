package user

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/subscription"
	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// GetUserTrafficServersHandler documents Get User Traffic Servers.
//
// @Summary Get User Traffic Servers
// @Tags user
// @Produce json
// @Security BearerAuth
// @Param request query dto.TrafficServersRequest true "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.TrafficServersResponse}
// @Router /v1/public/user/traffic/servers [get]
func GetUserTrafficServersHandler(service subscription.Service) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.TrafficServersRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			httpx.ParamErrorResult(ctx, err)
			return
		}
		if err := validation.Validate(&req); err != nil {
			httpx.ParamErrorResult(ctx, err)
			return
		}
		resp, err := service.UserTrafficServers(c, &req)
		httpx.HttpResult(ctx, resp, err)
	}
}
