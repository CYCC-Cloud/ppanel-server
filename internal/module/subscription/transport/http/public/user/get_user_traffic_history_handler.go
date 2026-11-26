package user

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/perfect-panel/backend/internal/module/subscription"
	dto "github.com/perfect-panel/backend/internal/module/subscription/contract"
	"github.com/perfect-panel/backend/internal/transport/http/validation"
	"github.com/perfect-panel/backend/pkg/httpx"
)

// GetUserTrafficHistoryHandler documents Get User Traffic History.
//
// @Summary Get User Traffic History
// @Tags user
// @Produce json
// @Security BearerAuth
// @Param request query dto.TrafficHistoryRequest true "Request parameters"
// @Success 200 {object} httpx.ResponseSuccessBean{data=dto.TrafficHistoryResponse}
// @Router /v1/public/user/traffic/history [get]
func GetUserTrafficHistoryHandler(service subscription.Service) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		var req dto.TrafficHistoryRequest
		if err := httpx.ShouldBind(ctx, &req); err != nil {
			httpx.ParamErrorResult(ctx, err)
			return
		}
		if err := validation.Validate(&req); err != nil {
			httpx.ParamErrorResult(ctx, err)
			return
		}
		resp, err := service.UserTrafficHistory(c, &req)
		httpx.HttpResult(ctx, resp, err)
	}
}
