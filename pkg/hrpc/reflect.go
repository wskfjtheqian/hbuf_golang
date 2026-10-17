package hrpc

import (
	"context"
	"encoding/json"
	"reflect"
	"runtime"
	"strings"

	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
)

// GetServerAndFuncName 获得指定服务和方法的名字
func GetServerAndFuncName(f any) (string, string, error) {
	str := runtime.FuncForPC(reflect.ValueOf(f).Pointer()).Name()
	var method string
	if i := strings.LastIndex(str, "."); i >= 0 {
		method = str[i+1:]
		str = str[:i]
		if i := strings.LastIndex(str, "."); i >= 0 {
			return str[i+1:], method, nil
		}
	}

	return "", "", herror.NewError("invalid server function")
}

// GetServerFuncName 获得指定方法的名字
func GetServerFuncName(f any) string {
	name := runtime.FuncForPC(reflect.ValueOf(f).Pointer()).Name()
	if i := strings.LastIndex(name, "."); i >= 0 {
		name = name[i+1:]
	}
	return name
}

// InvokeServerFunc 通过反射调用方法,args 为 JSON 字符串。
// 返回值约定:
//   - 0 个返回值:视为成功,返回 (nil, nil)
//   - 1 个返回值:必须为 error
//   - 2 个返回值:第一个为数据,第二个必须为 error
//
// 不符合约定时返回明确错误,不 panic。
func InvokeServerFunc(ctx context.Context, obj any, method string, args string) (any, error) {
	m := reflect.ValueOf(obj).MethodByName(method)
	if !m.IsValid() {
		return nil, herror.NewError("method not found")
	}

	paramCount := m.Type().NumIn()
	var params []reflect.Value
	if paramCount > 0 {
		params = append(params, reflect.ValueOf(ctx))
	}
	if paramCount > 1 {
		//通过反射获获得参数类型
		paramType := m.Type().In(1)
		//通过反射创建参数对象
		param := reflect.New(paramType).Interface()
		//将 JSON 字符串反序列化到参数对象中
		err := json.Unmarshal([]byte(args), param)
		if err != nil {
			return nil, herror.Wrap(err)
		}
		params = append(params, reflect.ValueOf(param).Elem())
	}

	//调用函数
	results := m.Call(params)
	if len(results) == 0 {
		return nil, nil
	}
	if len(results) == 1 {
		if v := results[0].Interface(); v != nil {
			if err, ok := v.(error); ok {
				return nil, err
			}
			return nil, herror.NewError("method " + method + " must return error as its only value")
		}
		return nil, nil
	}
	if len(results) > 2 {
		return nil, herror.NewError("method " + method + " returns more than 2 values")
	}
	if v := results[1].Interface(); v != nil {
		err, ok := v.(error)
		if !ok {
			return nil, herror.NewError("method " + method + " second return value must be error")
		}
		return nil, err
	}
	return results[0].Interface(), nil
}
