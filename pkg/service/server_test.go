package service_test

import (
	"github.com/wskfjtheqian/hbuf_golang/pkg/etcd"
	"github.com/wskfjtheqian/hbuf_golang/pkg/service"
	"github.com/wskfjtheqian/hbuf_golang/pkg/utl"
	"testing"
)

// 测试服务注册和注销的单元测试
func TestService_RegisterAndDeregister(t *testing.T) {
	// 创建一个 Etcd 客户端
	e := etcd.NewEtcd()
	err := e.SetConfig(&etcd.Config{
		Endpoints: []string{"192.168.1.202:2379"},
	})
	if err != nil {
		t.Error("创建 Etcd 客户端失败", err)
		return
	}

	// 创建一个新的Service实例
	service := serviceNewService(e)
	service.install = map[string]struct{}{
		"user":  {},
		"order": {},
	}
	// 创建测试配置
	cfg := &Config{
		Server: &Server{
			Register: true,
			Local:    true,
			Http: &Http{
				Path: utl.ToPointer("/"),
			},
		},
	}

	// 设置服务配置
	err = service.SetConfig(cfg)
	if err != nil {
		t.Error("设置服务配置失败", err)
	}

	select {}
}

// 测试注销时未注册服务的边界情况
func TestService_Deregister_NoRegistration(t *testing.T) {
	// 创建一个 Etcd 客户端
	e := etcd.NewEtcd()
	err := e.SetConfig(&etcd.Config{
		Endpoints: []string{"192.168.1.202:2379"},
	})
	if err != nil {
		t.Error("创建 Etcd 客户端失败", err)
		return
	}

	// 创建一个新的Service实例
	service := service.NewService(e)
	service.install = map[string]struct{}{
		"user":  {},
		"order": {},
	}
	// 创建测试配置
	cfg := &Config{
		Server: &Server{
			Register: true,
			Local:    true,
			Http: &Http{
				Path: utl.ToPointer("/"),
			},
		},
	}

	// 设置服务配置
	err = service.SetConfig(cfg)
	if err != nil {
		t.Error("设置服务配置失败", err)
	}

	select {}
}

// 测试注册未设置配置的情况
func TestService_Register_NoConfig(t *testing.T) {

}

// 测试服务发现功能
func TestService_Discovery(t *testing.T) {
	//service := NewService()
	//
	//cfg := &Config{
	//	Server: &ServerConfig{
	//		Name:      "testService",
	//		Addr:      "127.0.0.1",
	//		Port:      8080,
	//		TTL:       60,
	//		RegisterEtcd:  true,
	//		LeaseTime: 60,
	//	},
	//}
	//
	//err := service.SetConfig(cfg)
	//assert.NoError(t, err)
	//
	//// 注册服务以便进行发现
	//err = service.RegisterEtcd(context.Background())
	//assert.NoError(t, err, "注册服务失败")
	//
	//// 启动服务发现
	//err = service.Discovery(context.Background())
	//assert.NoError(t, err, "服务发现失败")
}

// 测试服务注册时 Etcd 客户端错误的情况
func TestService_Register_EtcdClientError(t *testing.T) {
	//service := NewService()
	//service.etcd = etcd.Etcd{
	//	// 模拟返回错误的 Etcd 客户端
	//	GetClientFunc: func() (clientv3.Client, error) {
	//		return nil, erro.NewError("etcd client error")
	//	},
	//}
	//
	//cfg := &Config{
	//	Server: &ServerConfig{
	//		RegisterEtcd: true,
	//	},
	//}
	//
	//err := service.SetConfig(cfg)
	//assert.NoError(t, err)
	//
	//err = service.RegisterEtcd(context.Background())
	//assert.Error(t, err, "注册服务时应返回 Etcd 客户端错误")
	//assert.Equal(t, "etcd client error", err.Error())
}

// 测试服务注销时 Etcd 客户端错误的情况
func TestService_Deregister_EtcdClientError(t *testing.T) {
	//service := NewService()
	//service.etcd = etcd.Etcd{
	//	// 模拟返回错误的 Etcd 客户端
	//	GetClientFunc: func() (clientv3.Client, error) {
	//		return nil, erro.NewError("etcd client error")
	//	},
	//}
	//
	//err := service.Deregister(context.Background())
	//assert.Error(t, err, "注销服务时应返回 Etcd 客户端错误")
	//assert.Equal(t, "etcd client error", err.Error())
}

// 测试租约失败的情况
func TestService_Register_LeaseError(t *testing.T) {
	//service := NewService()
	//
	//service.etcd = etcd.Etcd{
	//	// 模拟返回错误的 Etcd 客户端
	//	GetClientFunc: func() (clientv3.Client, error) {
	//		return &clientv3.Client{}, nil
	//	},
	//}
	//
	//cfg := &Config{
	//	Server: &ServerConfig{
	//		RegisterEtcd:  true,
	//		LeaseTime: 60,
	//	},
	//}
	//
	//err := service.SetConfig(cfg)
	//assert.NoError(t, err)
	//
	//// 这里我们模拟租约申请失败
	//err = service.RegisterEtcd(context.Background())
	//assert.Error(t, err, "注册服务时应返回租约申请错误")
}

// 测试没有配置的情况
func TestService_NoConfig(t *testing.T) {
	//service := NewService()
	//
	//err := service.SetConfig(nil)
	//assert.NoError(t, err, "设置为空配置应成功")
	//
	//err = service.Discovery(context.Background())
	//assert.NoError(t, err, "服务发现在没有配置的情况下应成功")
}
