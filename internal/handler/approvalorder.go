package handler

import (
	"time"

	"encoding/json"

	"github.com/gin-gonic/gin"
	"gorm.io/datatypes"
	"k8s.io/klog/v2"

	"github.com/raids-lab/crater/dao/model"
	"github.com/raids-lab/crater/dao/query"
	"github.com/raids-lab/crater/internal/resputil"
	"github.com/raids-lab/crater/internal/util"
)

//nolint:gochecknoinits // This is the standard way to register a gin handler.
func init() {
	Registers = append(Registers, NewApprovalOrderMgr)
}

type ApprovalOrderMgr struct {
	name string
}

func NewApprovalOrderMgr(_ *RegisterConfig) Manager {
	return &ApprovalOrderMgr{
		name: "approvalorder",
	}
}
func (mgr *ApprovalOrderMgr) GetName() string { return mgr.name }

func (mgr *ApprovalOrderMgr) RegisterPublic(_ *gin.RouterGroup) {}

func (mgr *ApprovalOrderMgr) RegisterProtected(g *gin.RouterGroup) {
	// RESTful 风格的路由设计
	g.GET("", mgr.GetMyApprovalOrders)        // 获取我的审批工单列表
	g.GET("/:id", mgr.GetApprovalOrder)       // 通过ID获取审批工单详情
	g.POST("", mgr.CreateApprovalOrder)       // 创建审批工单
	g.PUT("/:id", mgr.UpdateApprovalOrder)    // 更新审批工单
	g.DELETE("/:id", mgr.DeleteApprovalOrder) // 删除审批工单
}

func (mgr *ApprovalOrderMgr) RegisterAdmin(g *gin.RouterGroup) {
	// 管理员接口
	g.GET("", mgr.ListAllApprovalOrders)     // 获取所有审批工单
	g.GET("/:id", mgr.GetApprovalOrderAdmin) // 管理员通过ID获取审批工单详情
}

type ApprovalOrderResp struct {
	ID          uint                       `json:"id"`
	Name        string                     `json:"name"`
	Type        model.ApprovalOrderType    `json:"type"`
	Status      model.ApprovalOrderStatus  `json:"status"`
	Content     model.ApprovalOrderContent `json:"content"`
	ReviewNotes string                     `json:"reviewNotes"`
	CreatedAt   time.Time                  `json:"createdAt"`

	CreatorID  uint           `json:"creatorID"`
	Creator    model.UserInfo `json:"creator"`
	ReviewerID uint           `json:"reviewerID"`
	Reviewer   model.UserInfo `json:"reviewer"`
}

// swagger
//
//	@Summary		获取我的审批工单
//	@Description	获取当前用户创建的所有审批工单
//	@Tags			approvalorder
//	@Accept			json
//	@Produce		json
//	@Security		Bearer
//	@Success		200	{object}	resputil.Response[any]	"成功返回工单列表"
//	@Failure		400	{object}	resputil.Response[any]	"请求参数错误"
//	@Failure		500	{object}	resputil.Response[any]	"服务器错误"
//	@Router			/v1/approvalorder [get]
//
// GetMyApprovalOrders 获取当前用户创建的所有审批工单
func (mgr *ApprovalOrderMgr) GetMyApprovalOrders(c *gin.Context) {
	// 1. 获取当前用户信息
	token := util.GetToken(c)
	if token.UserID == 0 {
		resputil.Error(c, "cannot get user id", resputil.NotSpecified)
		return
	}

	// 2. 查询当前用户作为创建人的所有工单
	ao := query.ApprovalOrder
	creatorOrders, err := ao.WithContext(c).
		Preload(ao.Creator).  // 预加载创建人信息
		Preload(ao.Reviewer). // 预加载审批人信息（可能为空）
		Where(ao.CreatorID.Eq(token.UserID)).
		Find()
	if err != nil {
		klog.Errorf("failed to query created approval orders, userID: %d, err: %v", token.UserID, err)
		resputil.Error(c, "failed to get my approval orders", resputil.NotSpecified)
		return
	}

	// 3. 转换为响应格式
	result := convertToApprovalOrderResps(creatorOrders)
	resputil.Success(c, result)
}

// 辅助函数：将 datatypes.JSONType 转换为 ApprovalOrderContent
func unmarshalApprovalOrderContent(content datatypes.JSONType[model.ApprovalOrderContent]) model.ApprovalOrderContent {
	var result model.ApprovalOrderContent
	jsonBytes, err := content.MarshalJSON()
	if err != nil {
		klog.Warningf("failed to marshal approval order content: %v", err)
		return model.ApprovalOrderContent{}
	}
	if err := json.Unmarshal(jsonBytes, &result); err != nil {
		klog.Warningf("failed to unmarshal approval order content: %v", err)
		return model.ApprovalOrderContent{}
	}
	return result
}

// 新增辅助函数：将 ApprovalOrder 数组转换为 ApprovalOrderResp 数组
func convertToApprovalOrderResps(orders []*model.ApprovalOrder) []ApprovalOrderResp {
	var result []ApprovalOrderResp
	for i := range orders {
		resp := ApprovalOrderResp{
			ID:          orders[i].ID,
			Name:        orders[i].Name,
			Type:        orders[i].Type,
			Status:      orders[i].Status,
			Content:     unmarshalApprovalOrderContent(orders[i].Content),
			ReviewNotes: orders[i].ReviewNotes,
			CreatedAt:   orders[i].CreatedAt,
			CreatorID:   orders[i].CreatorID,
			Creator: model.UserInfo{
				Username: orders[i].Creator.Name,
				Nickname: orders[i].Creator.Nickname,
			},
			ReviewerID: orders[i].ReviewerID,
		}

		// 处理审批人信息
		if orders[i].ReviewerID != 0 {
			resp.Reviewer = model.UserInfo{
				Username: orders[i].Reviewer.Name,
				Nickname: orders[i].Reviewer.Nickname,
			}
		}

		result = append(result, resp)
	}
	return result
}

// swagger
//
//	@Summary		展示所有审批订单
//	@Description	展示所有审批订单
//	@Tags			approvalorder
//	@Accept			json
//	@Produce		json
//	@Success		200 {object} resputil.Response[any] "成功返回值描述"
//	@Failure		400	{object} resputil.Response[any]	"请求参数错误"
//	@Failure		500	{object} resputil.Response[any] "服务器错误"
//	@Security		Bearer
//	@Router			/v1/admin/approvalorder [get]
func (mgr *ApprovalOrderMgr) ListAllApprovalOrders(c *gin.Context) {
	klog.Infof("List all approval orders")

	ao := query.ApprovalOrder
	allOrders, err := ao.WithContext(c).
		Preload(ao.Creator).  // 预加载创建人信息
		Preload(ao.Reviewer). // 预加载审批人信息（可能为空）
		Order(ao.CreatedAt.Desc()).
		Find()
	if err != nil {
		klog.Errorf("failed to query all approval orders, err: %v", err)
		resputil.Error(c, "failed to list all approval orders", resputil.NotSpecified)
		return
	}

	result := convertToApprovalOrderResps(allOrders)
	resputil.Success(c, result)
}

type ApprovalOrderreq struct {
	Name           string                    `json:"name" binding:"required"`
	Type           model.ApprovalOrderType   `json:"type" binding:"required"`
	Status         model.ApprovalOrderStatus `json:"status"`
	TypeID         uint                      `json:"approvalorderTypeID" `        // 关联的ID，可能是数据集或任务ID
	Reason         string                    `json:"approvalOrderReason" `        // 审批原因
	ExtensionHours uint                      `json:"approvalOrderExtensionHours"` // 延长小时数
}

// swagger
//
//	@Summary		创建审批工单
//	@Description	创建新的审批工单
//	@Tags			approvalorder
//	@Accept			json
//	@Produce		json
//	@Security		Bearer
//	@Param			body body ApprovalOrderreq true "审批工单信息"
//	@Success		200 {object} resputil.Response[string] "成功返回值描述"
//	@Failure		400 {object} resputil.Response[any] "请求参数错误"
//	@Failure		500 {object} resputil.Response[any] "服务器错误"
//	@Router			/v1/approvalorder [post]
func (mgr *ApprovalOrderMgr) CreateApprovalOrder(c *gin.Context) {
	var req ApprovalOrderreq
	if err := c.ShouldBindJSON(&req); err != nil {
		klog.Errorf("failed to bind request parameters: %v", err)
		resputil.Error(c, "invalid request parameters", resputil.NotSpecified)
		return
	}

	// 1. 获取当前用户信息
	token := util.GetToken(c)
	if token.UserID == 0 {
		resputil.Error(c, "cannot get user id", resputil.NotSpecified)
		return
	}

	// 2. 创建审批工单
	order := model.ApprovalOrder{
		Name:   req.Name,
		Type:   req.Type,
		Status: model.ApprovalOrderStatusPending,
		Content: datatypes.NewJSONType(model.ApprovalOrderContent{
			ApprovalOrderTypeID:         req.TypeID,
			ApprovalOrderExtensionHours: req.ExtensionHours,
			ApprovalOrderReason:         req.Reason,
		}),
		CreatorID: token.UserID,
	}

	if err := query.ApprovalOrder.WithContext(c).Create(&order); err != nil {
		klog.Errorf("failed to create approval order, userID: %d, err: %v", token.UserID, err)
		resputil.Error(c, "failed to create approval order", resputil.NotSpecified)
		return
	}
	resputil.Success(c, "create approvalorder successfully")
}

type UpdateApprovalOrder struct {
	Name           string                    `json:"name" binding:"required"`
	Type           model.ApprovalOrderType   `json:"type" binding:"required"`
	Status         model.ApprovalOrderStatus `json:"status"`
	TypeID         uint                      `json:"approvalorderTypeID" `        // 关联的ID，可能是数据集或任务ID
	Reason         string                    `json:"approvalOrderReason" `        // 审批原因
	ExtensionHours uint                      `json:"approvalOrderExtensionHours"` // 延长小时数
	ReviewerID     uint                      `json:"reviewerID"`                  // 审批人ID
	ReviewNotes    string                    `json:"reviewNotes"`                 // 审批备注
}
type ApprovalOrderIDReq struct {
	ID uint `uri:"id" binding:"required"` // 工单ID
}

// swagger
//
//	@Summary		更新审批工单
//	@Description	更新现有的审批工单
//	@Tags			approvalorder
//	@Accept			json
//	@Produce		json
//	@Security		Bearer
//	@Param			body body UpdateApprovalOrder true "审批工单信息"
//	@Success		200 {object} resputil.Response[string] "成功返回值描述"
//	@Failure		400 {object} resputil.Response[any] "请求参数错误"
//	@Failure		500 {object} resputil.Response[any] "服务器错误"
//	@Router			/v1/approvalorder/{id} [put]
func (mgr *ApprovalOrderMgr) UpdateApprovalOrder(c *gin.Context) {
	var req UpdateApprovalOrder
	var orderID ApprovalOrderIDReq
	if err := c.ShouldBindJSON(&req); err != nil {
		klog.Errorf("failed to bind request parameters: %v", err)
		resputil.Error(c, "invalid request parameters", resputil.NotSpecified)
		return
	}
	if err := c.ShouldBindUri(&orderID); err != nil {
		klog.Errorf("failed to bind request parameters: %v", err)
		resputil.Error(c, "invalid request parameters", resputil.NotSpecified)
		return
	}
	// 1. 获取当前用户信息
	token := util.GetToken(c)
	if token.UserID == 0 {
		resputil.Error(c, "cannot get user id", resputil.NotSpecified)
		return
	}

	// 2. 更新审批工单
	order := model.ApprovalOrder{
		Name:   req.Name,
		Type:   req.Type,
		Status: req.Status,
		Content: datatypes.NewJSONType(model.ApprovalOrderContent{
			ApprovalOrderTypeID:         req.TypeID,
			ApprovalOrderExtensionHours: req.ExtensionHours,
			ApprovalOrderReason:         req.Reason,
		}),
		ReviewerID:  req.ReviewerID,
		ReviewNotes: req.ReviewNotes,
	}

	info, err := query.ApprovalOrder.WithContext(c).Where(query.ApprovalOrder.ID.Eq(orderID.ID)).Updates(&order)
	if err != nil {
		klog.Errorf("failed to update approval order, userID: %d, err: %v", token.UserID, err)
		resputil.Error(c, "failed to update approval order", resputil.NotSpecified)
		return
	}
	klog.Infof("updated approval order successfully, affected rows: %d", info.RowsAffected)
	resputil.Success(c, "update approvalorder successfully")
}

// swagger
//
//	@Summary		删除审批工单
//	@Description	删除指定的审批工单
//	@Tags			approvalorder
//	@Accept			json
//	@Produce		json
//	@Security		Bearer
//	@Param			id path int true "工单ID"
//	@Success		200 {object} resputil.Response[string] "成功返回值描述"
//	@Failure		400 {object} resputil.Response[any] "请求参数错误"
//	@Failure		500 {object} resputil.Response[any] "服务器错误"
//	@Router			/v1/approvalorder/{id} [delete]
//
// DeleteApprovalOrder 用户删除指定的审批工单
func (mgr *ApprovalOrderMgr) DeleteApprovalOrder(c *gin.Context) {
	// 1. 获取当前用户信息
	token := util.GetToken(c)

	if token.UserID == 0 {
		resputil.Error(c, "cannot get user id", resputil.NotSpecified)
		return
	}

	// 2. 获取工单ID
	var orderID ApprovalOrderIDReq
	if err := c.ShouldBindUri(&orderID); err != nil {
		klog.Errorf("failed to bind request parameters: %v", err)
		resputil.Error(c, "failed to bind request parameters", resputil.NotSpecified)
		return
	}

	// 3. 首先检查工单是否存在且属于当前用户
	ao := query.ApprovalOrder
	existingOrder, err := ao.WithContext(c).
		Where(ao.ID.Eq(orderID.ID)).
		First()
	if err != nil {
		klog.Errorf("approval order not found, userID: %d, orderID: %d, err: %v", token.UserID, orderID, err)
		resputil.Error(c, "approval order not found", resputil.NotSpecified)
		return
	}

	// 4. 权限检查：只有创建者才能删除自己的工单
	if existingOrder.CreatorID != token.UserID {
		klog.Warningf("user attempted to delete an order not created by themselves, userID: %d, orderID: %d, creatorID: %d",
			token.UserID, orderID, existingOrder.CreatorID)
		resputil.Error(c, "permission denied to delete this approval order", resputil.NotSpecified)
		return
	}

	// 6. 执行删除操作
	result, err := ao.WithContext(c).Where(ao.ID.Eq(orderID.ID)).Delete(&model.ApprovalOrder{})
	if err != nil {
		klog.Errorf("failed to delete approval order, userID: %d, orderID: %d, err: %v", token.UserID, orderID, err)
		resputil.Error(c, "failed to delete approval order", resputil.NotSpecified)
		return
	}

	// 7. 检查是否真的删除了记录
	if result.RowsAffected == 0 {
		klog.Warningf("delete operation affected no records, userID: %d, orderID: %d", token.UserID, orderID)
		resputil.Error(c, "delete failed, record not found", resputil.NotSpecified)
		return
	}

	klog.Infof("deleted approval order successfully, userID: %d, orderID: %d", token.UserID, orderID)
	resputil.Success(c, "delete approvalorder successfully")
}

// swagger
//
//	@Summary		获取审批工单详情
//	@Description	通过ID获取审批工单详情（仅限创建者）
//	@Tags			approvalorder
//	@Accept			json
//	@Produce		json
//	@Security		Bearer
//	@Param			id path int true "工单ID"
//	@Success		200 {object} resputil.Response[ApprovalOrderResp] "成功返回工单详情"
//	@Failure		400 {object} resputil.Response[any] "请求参数错误"
//	@Failure		403 {object} resputil.Response[any] "权限不足"
//	@Failure		404 {object} resputil.Response[any] "工单不存在"
//	@Failure		500 {object} resputil.Response[any] "服务器错误"
//	@Router			/v1/approvalorder/{id} [get]
func (mgr *ApprovalOrderMgr) GetApprovalOrder(c *gin.Context) {
	// 1. 获取当前用户信息
	token := util.GetToken(c)
	if token.UserID == 0 {
		resputil.Error(c, "can not get user ID", resputil.NotSpecified)
		return
	}

	// 2. 获取工单ID
	var orderID ApprovalOrderIDReq
	if err := c.ShouldBindUri(&orderID); err != nil {
		klog.Errorf("failed to bind request parameters: %v", err)
		resputil.Error(c, "invalid request parameters", resputil.NotSpecified)
		return
	}

	// 3. 查询工单详情
	ao := query.ApprovalOrder
	order, err := ao.WithContext(c).
		Preload(ao.Creator).
		Preload(ao.Reviewer).
		Where(ao.ID.Eq(orderID.ID)).
		First()
	if err != nil {
		klog.Errorf("failed to query approval order, userID: %d, orderID: %d, err: %v", token.UserID, orderID, err)
		resputil.Error(c, "approval order not found", resputil.NotSpecified)
		return
	}

	// 4. 权限检查：只有创建者才能查看自己的工单
	if order.CreatorID != token.UserID {
		klog.Warningf("user attempted to view an order not created by themselves, userID: %d, orderID: %d, creatorID: %d",
			token.UserID, orderID.ID, order.CreatorID)
		resputil.Error(c, "permission denied to view this approval order", resputil.NotSpecified)
		return
	}

	// 5. 转换为响应格式
	result := convertToApprovalOrderResp(order)
	resputil.Success(c, result)
}

// swagger
//
//	@Summary		管理员获取审批工单详情
//	@Description	管理员通过ID获取任意审批工单详情
//	@Tags			approvalorder
//	@Accept			json
//	@Produce		json
//	@Security		Bearer
//	@Param			id path int true "工单ID"
//	@Success		200 {object} resputil.Response[ApprovalOrderResp] "成功返回工单详情"
//	@Failure		400 {object} resputil.Response[any] "请求参数错误"
//	@Failure		404 {object} resputil.Response[any] "工单不存在"
//	@Failure		500 {object} resputil.Response[any] "服务器错误"
//	@Router			/v1/admin/approvalorder/{id} [get]
func (mgr *ApprovalOrderMgr) GetApprovalOrderAdmin(c *gin.Context) {
	// 1. 获取工单ID
	var orderID ApprovalOrderIDReq
	if err := c.ShouldBindUri(&orderID); err != nil {
		klog.Errorf("bind uri error: %v", err)
		resputil.Error(c, "uri error", resputil.NotSpecified)
		return
	}

	// 2. 查询工单详情
	ao := query.ApprovalOrder
	order, err := ao.WithContext(c).
		Preload(ao.Creator).
		Preload(ao.Reviewer).
		Where(ao.ID.Eq(orderID.ID)).
		First()
	if err != nil {
		klog.Errorf("admin failed to query approval order, orderID: %d, err: %v", orderID.ID, err)
		resputil.Error(c, "approval order not found", resputil.NotSpecified)
		return
	}

	// 3. 转换为响应格式
	result := convertToApprovalOrderResp(order)
	resputil.Success(c, result)
}

// 新增辅助函数：将单个 ApprovalOrder 转换为 ApprovalOrderResp
func convertToApprovalOrderResp(order *model.ApprovalOrder) ApprovalOrderResp {
	resp := ApprovalOrderResp{
		ID:          order.ID,
		Name:        order.Name,
		Type:        order.Type,
		Status:      order.Status,
		Content:     unmarshalApprovalOrderContent(order.Content),
		ReviewNotes: order.ReviewNotes,
		CreatedAt:   order.CreatedAt,
		CreatorID:   order.CreatorID,
		Creator: model.UserInfo{
			Username: order.Creator.Name,
			Nickname: order.Creator.Nickname,
		},
		ReviewerID: order.ReviewerID,
	}

	// 处理审批人信息
	if order.ReviewerID != 0 {
		resp.Reviewer = model.UserInfo{
			Username: order.Reviewer.Name,
			Nickname: order.Reviewer.Nickname,
		}
	}

	return resp
}
