// Description: 生成所有表的 Model 结构体和 CRUD 代码
package main

import (
	"gorm.io/gen"

	"github.com/raids-lab/crater/dao/model"
	"github.com/raids-lab/crater/dao/query"
)

func main() {
	g := gen.NewGenerator(gen.Config{
		OutPath: "./dao/query",

		// gen.WithoutContext：禁用WithContext模式
		// gen.WithDefaultQuery：生成一个全局Query对象Q
		// gen.WithQueryInterface：生成Query接口
		Mode: gen.WithDefaultQuery | gen.WithQueryInterface,
	})

	// 通常复用项目中已有的SQL连接配置 db(*gorm.DB)
	g.UseDB(query.GetDB())

	// 从连接的数据库为所有表生成 Model 结构体和 CRUD 代码
	g.ApplyBasic(
		model.User{},
		model.Account{},
		model.UserAccount{},
		model.Dataset{},
		model.AccountDataset{},
		model.UserDataset{},
		model.Resource{},
		model.Job{},
		model.Image{},
		model.Kaniko{},
		model.Jobtemplate{},
		model.Alert{},
		model.AITask{},
		model.ResourceNetwork{},
		model.ResourceVGPU{},
		model.ImageUser{},
		model.ImageAccount{},
		model.CudaBaseImage{},
		model.ApprovalOrder{},
	)

	// 执行并生成代码
	g.Execute()
}
