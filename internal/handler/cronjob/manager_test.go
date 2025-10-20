package cronjob

import (
	"testing"

	. "github.com/bytedance/mockey"
	"github.com/gin-gonic/gin"
	"github.com/robfig/cron/v3"
	. "github.com/smartystreets/goconvey/convey"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/raids-lab/crater/dao/model"
	"github.com/raids-lab/crater/dao/query"
)

// 这是一个单元测试函数，你需要使用PathConvey封装Mock调用，
// 使用 (*T).Run分隔不同的测试用例。
func Test_CronJobManager(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(nil)

	t.Run("暂停未暂停的任务", func(t *testing.T) {
		PatchConvey("暂停未暂停的任务", t, func() {
			// 准备测试数据
			name := "test-job"
			jobType := model.CronJobTypeHTTPCall
			spec := "*/5 * * * *"
			suspend := true
			jobConfig := `{"method":"GET","url":"/api/test","payload":{}}`

			// 创建 CronJobManager 实例
			manager := &CronJobManager{
				name:    "cronjob",
				cron:    cron.New(),
				entries: make(map[string]*JobEntry),
			}

			// 在 entries 中添加一个已存在的任务
			manager.entries[name] = &JobEntry{
				EntryID: cron.EntryID(1),
				Name:    name,
				Spec:    spec,
				Type:    jobType,
				Suspend: false,
			}

			mockConf := &model.CronJobConfig{
				Name:    name,
				Type:    jobType,
				Spec:    spec,
				Suspend: false,
				Config:  datatypes.JSON(jobConfig),
				EntryID: 1,
			}

			Mock((*gorm.DB).Model).Return(&gorm.DB{}).Build()
			Mock((*gorm.DB).Where).Return(&gorm.DB{}).Build()
			Mock((*gorm.DB).First).To(func(dest interface{}, conds ...interface{}) *gorm.DB {
				if conf, ok := dest.(*model.CronJobConfig); ok {
					*conf = *mockConf
				}
				return &gorm.DB{}
			}).Build()
			Mock((*gorm.DB).Updates).Return(&gorm.DB{}).Build()
			Mock(query.GetDB).Return(&gorm.DB{}).Build()

			// Mock cron 方法
			Mock((*cron.Cron).Remove).Return().Build()

			// 执行测试
			err := manager.UpdateJob(ctx, name, jobType, spec, suspend, &jobConfig)

			// 验证
			So(err, ShouldBeNil)
			So(manager.entries[name], ShouldBeNil) // 任务应该被移除
		})
	})

	t.Run("启动已暂停的任务", func(t *testing.T) {
		PatchConvey("启动已暂停的任务", t, func() {
			// 准备测试数据
			name := "test-job"
			jobType := model.CronJobTypeHTTPCall
			spec := "*/5 * * * *"
			suspend := false
			jobConfig := `{"method":"GET","url":"/api/test","payload":{}}`

			// 创建 CronJobManager 实例
			manager := &CronJobManager{
				name:    "cronjob",
				cron:    cron.New(),
				entries: make(map[string]*JobEntry),
			}

			mockConf := &model.CronJobConfig{
				Name:    name,
				Type:    jobType,
				Spec:    spec,
				Suspend: true, // 之前是暂停状态
				Config:  datatypes.JSON(jobConfig),
				EntryID: 0,
			}

			Mock((*gorm.DB).Model).Return(&gorm.DB{}).Build()
			Mock((*gorm.DB).Where).Return(&gorm.DB{}).Build()
			Mock((*gorm.DB).First).To(func(dest interface{}, conds ...interface{}) *gorm.DB {
				if conf, ok := dest.(*model.CronJobConfig); ok {
					*conf = *mockConf
				}
				return &gorm.DB{}
			}).Build()
			Mock((*gorm.DB).Updates).Return(&gorm.DB{}).Build()
			Mock(query.GetDB).Return(&gorm.DB{}).Build()

			// Mock cron 方法
			Mock((*cron.Cron).AddFunc).Return(cron.EntryID(2), nil).Build()

			// 执行测试
			err := manager.UpdateJob(ctx, name, jobType, spec, suspend, &jobConfig)

			// 验证
			So(err, ShouldBeNil)
			So(manager.entries[name], ShouldNotBeNil) // 任务应该被添加
			So(manager.entries[name].EntryID, ShouldEqual, cron.EntryID(2))
			So(manager.entries[name].Spec, ShouldEqual, spec)
			So(manager.entries[name].Suspend, ShouldEqual, suspend)
		})
	})

	t.Run("更新运行中任务的配置", func(t *testing.T) {
		PatchConvey("更新运行中任务的配置", t, func() {
			// 准备测试数据
			name := "test-job"
			jobType := model.CronJobTypeHTTPCall
			oldSpec := "*/5 * * * *"
			newSpec := "*/10 * * * *"
			suspend := false
			jobConfig := `{"method":"POST","url":"/api/test","payload":{"key":"value"}}`

			// 创建 CronJobManager 实例
			manager := &CronJobManager{
				name:    "cronjob",
				cron:    cron.New(),
				entries: make(map[string]*JobEntry),
			}

			// 在 entries 中添加一个已存在的任务
			manager.entries[name] = &JobEntry{
				EntryID: cron.EntryID(1),
				Name:    name,
				Spec:    oldSpec,
				Type:    jobType,
				Suspend: false,
			}

			mockConf := &model.CronJobConfig{
				Name:    name,
				Type:    jobType,
				Spec:    oldSpec,
				Suspend: false,
				Config:  datatypes.JSON([]byte(`{"method":"GET","url":"/api/old","payload":{}}`)),
				EntryID: 1,
			}

			Mock((*gorm.DB).Model).Return(&gorm.DB{}).Build()
			Mock((*gorm.DB).Where).Return(&gorm.DB{}).Build()
			Mock((*gorm.DB).First).To(func(dest interface{}, conds ...interface{}) *gorm.DB {
				if conf, ok := dest.(*model.CronJobConfig); ok {
					*conf = *mockConf
				}
				return &gorm.DB{}
			}).Build()
			Mock((*gorm.DB).Updates).Return(&gorm.DB{}).Build()
			Mock(query.GetDB).Return(&gorm.DB{}).Build()

			// Mock cron 方法
			Mock((*cron.Cron).Remove).Return().Build()
			Mock((*cron.Cron).AddFunc).Return(cron.EntryID(3), nil).Build()

			// 执行测试
			err := manager.UpdateJob(ctx, name, jobType, newSpec, suspend, &jobConfig)

			// 验证
			So(err, ShouldBeNil)
			So(manager.entries[name], ShouldNotBeNil)
			So(manager.entries[name].EntryID, ShouldEqual, cron.EntryID(3))
			So(manager.entries[name].Spec, ShouldEqual, newSpec)
		})
	})

	t.Run("数据库查询失败", func(t *testing.T) {
		PatchConvey("数据库查询失败", t, func() {
			// 准备测试数据
			name := "test-job"
			jobType := model.CronJobTypeHTTPCall
			spec := "*/5 * * * *"
			suspend := false
			jobConfig := `{"method":"GET","url":"/api/test","payload":{}}`

			// 创建 CronJobManager 实例
			manager := &CronJobManager{
				name:    "cronjob",
				cron:    cron.New(),
				entries: make(map[string]*JobEntry),
			}

			Mock((*gorm.DB).Model).Return(&gorm.DB{}).Build()
			Mock((*gorm.DB).Where).Return(&gorm.DB{}).Build()
			Mock((*gorm.DB).First).To(func(dest interface{}, conds ...interface{}) *gorm.DB {
				return &gorm.DB{Error: gorm.ErrRecordNotFound}
			}).Build()
			Mock(query.GetDB).Return(&gorm.DB{}).Build()

			// 执行测试
			err := manager.UpdateJob(ctx, name, jobType, spec, suspend, &jobConfig)

			// 验证
			So(err, ShouldNotBeNil)
		})
	})

	t.Run("任务无需更新", func(t *testing.T) {
		PatchConvey("任务无需更新", t, func() {
			// 准备测试数据
			name := "test-job"
			jobType := model.CronJobTypeHTTPCall
			spec := "*/5 * * * *"
			suspend := false
			jobConfig := `{"method":"GET","url":"/api/test","payload":{}}`

			// 创建 CronJobManager 实例
			manager := &CronJobManager{
				name:    "cronjob",
				cron:    cron.New(),
				entries: make(map[string]*JobEntry),
			}

			// 在 entries 中添加一个已存在的任务，配置完全相同
			manager.entries[name] = &JobEntry{
				EntryID: cron.EntryID(1),
				Name:    name,
				Spec:    spec,
				Type:    jobType,
				Suspend: false,
			}

			mockConf := &model.CronJobConfig{
				Name:    name,
				Type:    jobType,
				Spec:    spec,
				Suspend: false,
				Config:  datatypes.JSON(jobConfig),
				EntryID: 1,
			}

			Mock((*gorm.DB).Model).Return(&gorm.DB{}).Build()
			Mock((*gorm.DB).Where).Return(&gorm.DB{}).Build()
			Mock((*gorm.DB).First).To(func(dest interface{}, conds ...interface{}) *gorm.DB {
				if conf, ok := dest.(*model.CronJobConfig); ok {
					*conf = *mockConf
				}
				return &gorm.DB{}
			}).Build()
			Mock(query.GetDB).Return(&gorm.DB{}).Build()

			// 执行测试 - 不传 jobConfig 参数，配置不变
			err := manager.UpdateJob(ctx, name, jobType, spec, suspend, nil)

			// 验证
			So(err, ShouldBeNil)
			So(manager.entries[name], ShouldNotBeNil)
			So(manager.entries[name].EntryID, ShouldEqual, cron.EntryID(1)) // EntryID 不变
		})
	})
}
