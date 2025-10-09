package hrpc

import (
	"context"
	"encoding/json"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"reflect"
	"runtime"
	"strings"
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

// InvokeServerFunc 通过反射获调用函数,参数为 JSON 字符串
func InvokeServerFunc(ctx context.Context, obj any, method string, args string) (any, error) {
	m := reflect.ValueOf(obj).MethodByName(method)
	if !m.IsValid() {
		return nil, herror.NewError("method not found")
	}

	paramCount := m.Type().Len()
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
		return nil, results[0].Interface().(error)
	}
	if !results[1].IsNil() {
		return nil, results[1].Interface().(error)
	}
	return results[0].Interface(), nil
}
