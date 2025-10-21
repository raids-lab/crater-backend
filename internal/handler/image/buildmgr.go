package image

import (
	"fmt"

	"github.com/gin-gonic/gin"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"github.com/raids-lab/crater/dao/model"
	"github.com/raids-lab/crater/dao/query"
	"github.com/raids-lab/crater/internal/resputil"
	"github.com/raids-lab/crater/internal/util"
)

// UserListKaniko godoc
//
//	@Summary		用户获取镜像构建信息
//	@Description	返回该用户所有的镜像构建数据
//	@Tags			ImagePack
//	@Accept			json
//	@Produce		json
//	@Security		Bearer
//	@Router			/v1/images/kaniko [GET]
func (mgr *ImagePackMgr) UserListKaniko(c *gin.Context) {
	var kanikos []*model.Kaniko
	var err error
	token := util.GetToken(c)
	k := query.Kaniko
	kanikos, err = k.WithContext(c).
		Where(k.UserID.Eq(token.UserID)).
		Preload(k.User).
		Order(k.CreatedAt.Desc()).
		Find()
	if err != nil {
		klog.Errorf("fetch kaniko entity failed, err:%v", err)
	}
	response := mgr.generateKanikoListResponse(kanikos)
	resputil.Success(c, response)
}

// AdminListKaniko godoc
//
//	@Summary		管理员获取相关镜像的功能
//	@Description	管理员获取所有镜像制作信息
//	@Tags			ImagePack
//	@Accept			json
//	@Produce		json
//	@Security		Bearer
//	@Router			/v1/admin/images/kaniko [GET]
func (mgr *ImagePackMgr) AdminListKaniko(c *gin.Context) {
	var kanikos []*model.Kaniko
	var err error
	kanikoQuery := query.Kaniko
	kanikos, err = kanikoQuery.WithContext(c).Preload(kanikoQuery.User).Order(kanikoQuery.CreatedAt.Desc()).Find()
	if err != nil {
		klog.Errorf("fetch kaniko entity failed, err:%v", err)
	}
	response := mgr.generateKanikoListResponse(kanikos)
	resputil.Success(c, response)
}

// DeleteKanikoByID godoc
//
//	@Summary		根据ID删除Kaniko entity
//	@Description	根据ID更新Kaniko的状态为Deleted，起到删除的功能
//	@Tags			ImagePack
//	@Accept			json
//	@Produce		json
//	@Security		Bearer
//	@Param			ID	body	uint	true	"删除镜像的ID"
//	@Router			/v1/images/kaniko/{id} [DELETE]
func (mgr *ImagePackMgr) DeleteKanikoByID(c *gin.Context) {
	token := util.GetToken(c)
	var err error
	var deleteKanikoRequest DeleteKanikoByIDRequest
	if err = c.ShouldBindUri(&deleteKanikoRequest); err != nil {
		msg := fmt.Sprintf("validate delete parameters failed, err %v", err)
		resputil.BadRequestError(c, msg)
		return
	}
	kanikoID := deleteKanikoRequest.ID
	if isSuccess, errorMsg := mgr.deleteKanikoByID(c, false, kanikoID, token.UserID); isSuccess {
		resputil.Success(c, "")
	} else {
		resputil.Error(c, errorMsg, resputil.NotSpecified)
	}
}

// UserDeleteKanikoByIDList godoc
//
//	@Summary		根据IDList删除Kaniko entity
//	@Description	遍历列表，根据ID更新Kaniko的状态为Deleted，起到删除的功能
//	@Tags			ImagePack
//	@Accept			json
//	@Produce		json
//	@Security		Bearer
//	@Param			IDList	body	[]uint	true	"删除kaniko的IDList"
//	@Router			/v1/images/deletekaniko [POST]
func (mgr *ImagePackMgr) UserDeleteKanikoByIDList(c *gin.Context) {
	var err error
	var deleteKanikoListRequest DeleteKanikoByIDListRequest
	if err = c.ShouldBindJSON(&deleteKanikoListRequest); err != nil {
		msg := fmt.Sprintf("validate delete parameters failed, err %v", err)
		resputil.BadRequestError(c, msg)
		return
	}
	flag := mgr.deleteKanikoByIDList(c, true, deleteKanikoListRequest.IDList)
	if flag {
		resputil.Success(c, "")
	} else {
		resputil.Error(c, "failed to delete kaniko", resputil.NotSpecified)
	}
}

// AdminDeleteKanikoByIDList godoc
//
//	@Summary		管理员模式下根据IDList删除Kaniko entity
//	@Description	管理员模式下遍历列表，根据ID更新Kaniko的状态为Deleted，起到删除的功能
//	@Tags			ImagePack
//	@Accept			json
//	@Produce		json
//	@Security		Bearer
//	@Param			IDList	body	[]uint	true	"删除kaniko的IDList"
//	@Router			/v1/admin/images/deletekaniko [POST]
func (mgr *ImagePackMgr) AdminDeleteKanikoByIDList(c *gin.Context) {
	var err error
	var deleteKanikoListRequest DeleteKanikoByIDListRequest
	if err = c.ShouldBindJSON(&deleteKanikoListRequest); err != nil {
		msg := fmt.Sprintf("validate delete parameters failed, err %v", err)
		resputil.BadRequestError(c, msg)
		return
	}
	flag := mgr.deleteKanikoByIDList(c, true, deleteKanikoListRequest.IDList)
	if flag {
		resputil.Success(c, "")
	} else {
		resputil.Error(c, "failed to delete kaniko", resputil.NotSpecified)
	}
}

func (mgr *ImagePackMgr) deleteKanikoByIDList(c *gin.Context, isAdminMode bool, kanikoIDList []uint) (isSuccess bool) {
	flag := true
	userID := util.GetToken(c).UserID
	for _, kanikoID := range kanikoIDList {
		if isSuccess, errorMsg := mgr.deleteKanikoByID(c, isAdminMode, kanikoID, userID); !isSuccess {
			flag = false
			klog.Errorf("delete kaniko failed, err:%v", errorMsg)
		}
	}
	return flag
}

func (mgr *ImagePackMgr) deleteKanikoByID(c *gin.Context, isAdminMode bool, kanikoID, userID uint) (isSuccess bool, errMsg string) {
	var kaniko *model.Kaniko
	var err error
	var errorMsg string
	flag := true
	k := query.Kaniko
	// 0. specify user & admin query
	kanikoQuery := k.WithContext(c)
	if !isAdminMode {
		kanikoQuery = kanikoQuery.Where(k.UserID.Eq(userID))
	}
	// 1. check if kaniko exist and have permission
	if kaniko, err = kanikoQuery.Where(k.ID.Eq(kanikoID)).First(); err != nil {
		errorMsg = fmt.Sprintf("kaniko not exist or have no permission %+v", err)
		klog.Error(errorMsg)
		return false, errorMsg
	}
	// 2. if kaniko is finished, delete image entity
	if kaniko.Status == model.BuildJobFinished {
		if _, err = query.Image.WithContext(c).Where(query.Image.ImagePackName.Eq(kaniko.ImagePackName)).Delete(); err != nil {
			errorMsg = fmt.Sprintf("delete image entity failed! err:%v", err)
			klog.Error(errorMsg)
		}
	}
	// 3. delete kaniko entity from database
	if _, err = k.WithContext(c).Where(k.ID.Eq(kanikoID)).Delete(); err != nil {
		errorMsg = fmt.Sprintf("delete kaniko entity failed! err:%v", err)
		klog.Error(errorMsg)
		return false, errorMsg
	}
	// 4. if buildkit finished, then delete image from harbor
	if kaniko.Status == model.BuildJobFinished {
		if err = mgr.imageRegistry.DeleteImageFromProject(c, kaniko.ImageLink); err != nil {
			errorMsg = fmt.Sprintf("delete imagepack artifact failed! err:%+v", err)
			klog.Error(errorMsg)
		}
	}
	return flag, errorMsg
}

// UserCancelKanikoByID godoc
//
//	@Summary		取消镜像制作任务
//	@Description	根据ID取消镜像制作任务；若任务状态为 Finished/Failed 则提示镜像制作已结束
//	@Tags			ImagePack
//	@Accept			json
//	@Produce		json
//	@Security		Bearer
//	@Param			id	path	uint	true	"镜像构建任务ID"
//	@Router			/v1/images/cancel [POST]
func (mgr *ImagePackMgr) UserCancelKanikoByID(c *gin.Context) {
	token := util.GetToken(c)
	var req DeleteKanikoByIDRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		msg := fmt.Sprintf("validate cancel parameters failed, err %v", err)
		resputil.BadRequestError(c, msg)
		return
	}

	if ok, msg := mgr.cancelKanikoByID(c, false, req.ID, token.UserID); ok {
		resputil.Success(c, "")
	} else {
		resputil.Error(c, msg, resputil.NotSpecified)
	}
}

// cancelKanikoByID 取消镜像构建任务：任务已结束则提示，未结束则删除对应Job并将记录状态置为 Canceled
func (mgr *ImagePackMgr) cancelKanikoByID(c *gin.Context, isAdminMode bool, kanikoID, userID uint) (isSuccess bool, msg string) {
	k := query.Kaniko

	// 构造查询并进行权限过滤
	kanikoQuery := k.WithContext(c)
	if !isAdminMode {
		kanikoQuery = kanikoQuery.Where(k.UserID.Eq(userID))
	}

	// 获取 Kaniko 记录
	kaniko, err := kanikoQuery.Where(k.ID.Eq(kanikoID)).First()
	if err != nil || kaniko == nil {
		msg := fmt.Sprintf("kaniko not exist or have no permission %+v", err)
		klog.Error(msg)
		return false, msg
	}

	// 若任务已结束（Finished/Failed/Canceled），提示已结束
	if kaniko.Status == model.BuildJobFinished || kaniko.Status == model.BuildJobFailed || kaniko.Status == model.BuildJobCanceled {
		return false, "image build already ended"
	}

	// 删除对应的 Job（忽略错误，仅记录日志）
	if err := mgr.imagePacker.DeleteJob(c, kaniko.ImagePackName, UserNameSpace); err != nil {
		klog.Errorf("delete kaniko job failed! err:%v", err)
	}

	// 更新状态为 Canceled
	// if _, err := k.WithContext(c).Where(k.ID.Eq(kanikoID)).Update(k.Status, model.BuildJobCanceled); err != nil {
	// 	msg := fmt.Sprintf("update kaniko status to Canceled failed! err:%v", err)
	// 	klog.Error(msg)
	// 	return false, msg
	// }

	return true, ""
}

// GetKanikoByImagePackName godoc
//
//	@Summary		获取imagepack的详细信息
//	@Description	获取imagepackname，搜索到imagepack
//	@Tags			ImagePack
//	@Accept			json
//	@Produce		json
//	@Security		Bearer
//	@Param			name	query	string	true	"获取ImagePack的name"
//	@Router			/v1/images/getbyname [GET]
func (mgr *ImagePackMgr) GetKanikoByImagePackName(c *gin.Context) {
	kanikoQuery := query.Kaniko
	var req GetKanikoRequest
	var err error
	if err = c.ShouldBindQuery(&req); err != nil {
		msg := fmt.Sprintf("validate image get parameters failed, err %v", err)
		resputil.BadRequestError(c, msg)
		return
	}
	var kaniko *model.Kaniko
	if kaniko, err = kanikoQuery.WithContext(c).
		Where(kanikoQuery.ImagePackName.Eq(req.ImagePackName)).
		First(); err != nil {
		msg := fmt.Sprintf("fetch kaniko by name failed, err %v", err)
		resputil.BadRequestError(c, msg)
		return
	}

	podName, podNameSpace := mgr.getPodName(c, kaniko.ID)
	getKanikoResponse := GetKanikoResponse{
		ID:            kaniko.ID,
		ImageLink:     kaniko.ImageLink,
		Status:        kaniko.Status,
		BuildSource:   kaniko.BuildSource,
		CreatedAt:     kaniko.CreatedAt,
		ImagePackName: kaniko.ImagePackName,
		Description:   *kaniko.Description,
		Dockerfile:    *kaniko.Dockerfile,
		PodName:       podName,
		PodNameSpace:  podNameSpace,
	}
	resputil.Success(c, getKanikoResponse)
}

// GetKanikoTemplateByImagePackName godoc
//
//	@Summary		获取imagepack的模板信息
//	@Description	获取imagepackname，搜索到imagepack的模板信息
//	@Tags			ImagePack
//	@Accept			json
//	@Produce		json
//	@Security		Bearer
//	@Param			name	query	string	true	"获取ImagePack的name"
//	@Router			/v1/images/get [GET]
func (mgr *ImagePackMgr) GetKanikoTemplateByImagePackName(c *gin.Context) {
	kanikoQuery := query.Kaniko
	var req GetKanikoRequest
	var err error
	if err = c.ShouldBindQuery(&req); err != nil {
		msg := fmt.Sprintf("validate image get parameters failed, err %v", err)
		resputil.BadRequestError(c, msg)
		return
	}
	var kaniko *model.Kaniko
	if kaniko, err = kanikoQuery.WithContext(c).
		Where(kanikoQuery.ImagePackName.Eq(req.ImagePackName)).
		First(); err != nil {
		msg := fmt.Sprintf("fetch kaniko by name failed, err %v", err)
		resputil.BadRequestError(c, msg)
		return
	}

	resputil.Success(c, kaniko.Template)
}

// GetImagepackPodName godoc
//
//	@Summary		获取镜像构建Pod名称
//	@Description	根据ID获取镜像构建Pod名称和命名空间
//	@Tags			ImagePack
//	@Accept			json
//	@Produce		json
//	@Security		Bearer
//	@Param			id	query	uint	true	"镜像构建任务ID"
//	@Router			/v1/images/podname [GET]
func (mgr *ImagePackMgr) GetImagepackPodName(c *gin.Context) {
	var req GetKanikoPodRequest
	var err error
	if err = c.ShouldBindQuery(&req); err != nil {
		msg := fmt.Sprintf("validate image get parameters failed, err %v", err)
		resputil.BadRequestError(c, msg)
		return
	}
	podName, podNameSpace := mgr.getPodName(c, req.ID)
	resp := GetKanikoPodResponse{
		PodName:      podName,
		PodNameSpace: podNameSpace,
	}
	resputil.Success(c, resp)
}

func (mgr *ImagePackMgr) getPodName(c *gin.Context, kanikoID uint) (name, ns string) {
	var kaniko *model.Kaniko
	var err error
	kanikoQuery := query.Kaniko
	if kaniko, err = kanikoQuery.WithContext(c).
		Where(kanikoQuery.ID.Eq(kanikoID)).
		First(); err != nil {
		msg := fmt.Sprintf("fetch kaniko by name failed, err %v", err)
		resputil.BadRequestError(c, msg)
		return "", UserNameSpace
	}
	var pod *corev1.Pod
	pod, err = mgr.imagepackClient.GetImagePackPod(c, kaniko.ImagePackName, UserNameSpace)
	if err != nil || pod == nil {
		return "", UserNameSpace
	}
	return pod.Name, UserNameSpace
}

func (mgr *ImagePackMgr) generateKanikoListResponse(kanikos []*model.Kaniko) ListKanikoResponse {
	kanikoInfos := []KanikoInfo{}
	for i := range kanikos {
		kaniko := kanikos[i]
		archs := kaniko.Archs.Data()
		// TODO: remove temporary fix for empty archs
		if kaniko.Archs.Data() == nil {
			archs = []string{"linux/amd64"}
		}
		kanikoInfo := KanikoInfo{
			ID:          kaniko.ID,
			ImageLink:   kaniko.ImageLink,
			Status:      kaniko.Status,
			CreatedAt:   kaniko.CreatedAt,
			Size:        kaniko.Size,
			Description: *kaniko.Description,
			UserInfo: model.UserInfo{
				Username: kaniko.User.Name,
				Nickname: kaniko.User.Nickname,
			},
			Tags:          kaniko.Tags.Data(),
			Archs:         archs,
			ImagePackName: kaniko.ImagePackName,
			BuildSource:   kaniko.BuildSource,
		}
		kanikoInfos = append(kanikoInfos, kanikoInfo)
	}
	return ListKanikoResponse{
		KanikoInfoList: kanikoInfos,
	}
}
