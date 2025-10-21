package operations

import (
	"encoding/json"

	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
	"k8s.io/utils/ptr"

	cj "github.com/raids-lab/crater/internal/handler/cronjob"

	"github.com/raids-lab/crater/dao/model"
	"github.com/raids-lab/crater/internal/resputil"
)

type CronjobConfigs struct {
	Name     string         `json:"name"`
	Type     string         `json:"type"`
	Schedule string         `json:"schedule"`
	Suspend  bool           `json:"suspend"`
	Configs  map[string]any `json:"configs"`
}

// UpdateCronjobConfig godoc
//
//	@Summary		Update cronjob config
//	@Description	Update one cronjob config
//	@Tags			Operations
//	@Accept			json
//	@Produce		json
//	@Security		Bearer
//	@Param			use	body		CronjobConfigs			true	"CronjobConfigs"
//	@Success		200	{object}	resputil.Response[any]	"Success"
//	@Failure		400	{object}	resputil.Response[any]	"Request parameter error"
//	@Failure		500	{object}	resputil.Response[any]	"Other errors"
//	@Router			/v1/operations/cronjob [put]
func (mgr *OperationsMgr) UpdateCronjobConfig(c *gin.Context) {
	var req CronjobConfigs
	if err := c.ShouldBindJSON(&req); err != nil {
		resputil.Error(c, err.Error(), resputil.NotSpecified)
		return
	}

	var (
		jobTypePtr *model.CronJobType
		specPtr    *string
		configPtr  *string
	)
	if req.Type != "" {
		jobTypePtr = ptr.To(model.CronJobType(req.Type))
	}
	if req.Schedule != "" {
		specPtr = ptr.To(req.Schedule)
	}

	if len(req.Configs) > 0 {
		configJson, err := json.Marshal(req.Configs)
		if err != nil {
			resputil.Error(c, err.Error(), resputil.NotSpecified)
		}
		configPtr = ptr.To(string(configJson))
	}
	if err := cj.GetCronJobManager().UpdateJob(c, req.Name, jobTypePtr, specPtr, &req.Suspend, configPtr); err != nil {
		resputil.Error(c, err.Error(), resputil.NotSpecified)
		return
	}
	resputil.Success(c, "Successfully update cronjob config")
}

// GetCronjobConfigs godoc
//
//	@Summary		Get all cronjob configs
//	@Description	Get all cronjob configs
//	@Tags			Operations
//	@Accept			json
//	@Produce		json
//	@Security		Bearer
//	@Success		200	{object}	resputil.Response[any]	"Success"
//	@Failure		400	{object}	resputil.Response[any]	"Request parameter error"
//	@Failure		500	{object}	resputil.Response[any]	"Other errors"
//	@Router			/v1/operations/cronjob [get]
func (mgr *OperationsMgr) GetCronjobConfigs(c *gin.Context) {
	jobs, err := cj.GetCronJobManager().GetAllCronJobs(c)
	if err != nil {
		resputil.Error(c, err.Error(), resputil.NotSpecified)
		return
	}
	configs := lo.Map(jobs, func(job *model.CronJobConfig, _ int) CronjobConfigs {
		config := make(map[string]any)
		if err := json.Unmarshal(job.Config, &config); err != nil {
			config = map[string]any{}
		}
		ret := CronjobConfigs{
			Name:     job.Name,
			Type:     string(job.Type),
			Schedule: job.Spec,
			Suspend:  job.GetSuspend(),
			Configs:  config,
		}
		return ret
	})
	resputil.Success(c, configs)
}
