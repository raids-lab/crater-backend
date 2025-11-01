package cronjob

import (
	"testing"

	. "github.com/bytedance/mockey"
	. "github.com/smartystreets/goconvey/convey"
	"gorm.io/datatypes"
	"k8s.io/utils/ptr"

	"github.com/raids-lab/crater/dao/model"
)

func TestCronJob(t *testing.T) {
	t.Run("newInternalJobFunc", func(t *testing.T) {
		manager := NewCronJobManager(nil, nil, nil)

		PatchConvey("newInternalJobFunc", t, func() {
			jobName := CLEAN_LONG_TIME_RUNNING_JOB
			jobConfig := datatypes.JSON(`{"batchDays": 4, "interactiveDays": 4}`)
			jobFunc, err := manager.newInternalJobFunc(jobName, jobConfig)
			So(err, ShouldBeNil)
			So(jobFunc, ShouldNotBeNil)

			jobName = CLEAN_LOW_GPU_USAGE_JOB
			jobConfig = datatypes.JSON(`{"util": 0, "waitTime": 30, "timeRange": 90}`)
			jobFunc, err = manager.newInternalJobFunc(jobName, jobConfig)
			So(err, ShouldBeNil)
			So(jobFunc, ShouldNotBeNil)

			jobName = CLEAN_WAITING_JUPYTER_JOB
			jobConfig = datatypes.JSON(`{"waitMinitues": 5}`)
			jobFunc, err = manager.newInternalJobFunc(jobName, jobConfig)
			So(err, ShouldBeNil)
			So(jobFunc, ShouldNotBeNil)

			jobName = "unknown"
			jobConfig = datatypes.JSON(`{"unknown": "unknown"}`)
			jobFunc, err = manager.newInternalJobFunc(jobName, jobConfig)
			So(err, ShouldNotBeNil)
			So(jobFunc, ShouldBeNil)
		})
	})

	t.Run("prepareUpdateConfig", func(t *testing.T) {
		PatchConvey("prepareUpdateConfig", t, func() {
			manager := NewCronJobManager(nil, nil, nil)
			cur := &model.CronJobConfig{
				Name:    "test",
				Type:    model.CronJobTypeInternalFunc,
				Spec:    "0 0 * * *",
				Suspend: ptr.To(false),
				Config:  datatypes.JSON(`{"test": "test"}`),
			}
			update := manager.prepareUpdateConfig(
				cur,
				ptr.To(model.CronJobTypeInternalFunc),
				ptr.To("1 1 * * *"),
				ptr.To(true),
				ptr.To(`{"test": "test"}`),
			)
			So(update, ShouldNotBeNil)
			So(update.Name, ShouldEqual, "test")
			So(update.Type, ShouldEqual, model.CronJobTypeInternalFunc)
			So(update.Spec, ShouldEqual, "1 1 * * *")
			So(*update.Suspend, ShouldEqual, true)
			So(update.Config, ShouldEqual, datatypes.JSON(`{"test": "test"}`))
		})
	})
}
