package operations

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"

	"github.com/gin-gonic/gin"
	"k8s.io/klog/v2"

	"github.com/raids-lab/crater/internal/handler"
)

func MergeURLWithQuery(baseURL string, queryParams map[string]string) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("url.Parse failed to parse: %w", err)
	}
	q := u.Query()
	for key, value := range queryParams {
		q.Add(key, value)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func GetAdminTokenByLogin(_ *gin.Context, username, password string, serverHandler http.Handler) (string, error) {
	authReq := httptest.NewRequest(
		"POST", "/api/auth/login",
		bytes.NewBuffer([]byte(fmt.Sprintf("{\"auth\":\"normal\",\"username\":%q,\"password\":%q}", username, password))),
	)
	authReq.Header.Set("Content-Type", "application/json")
	authReq.Header.Set("accept", "application/json")
	authRecorder := httptest.NewRecorder()
	serverHandler.ServeHTTP(authRecorder, authReq)
	type AuthResp struct {
		Code int               `json:"code"`
		Data handler.LoginResp `json:"data"`
		Msg  string            `json:"msg"`
	}
	authResp := &AuthResp{}
	if err := json.Unmarshal(authRecorder.Body.Bytes(), authResp); err != nil {
		err := fmt.Errorf("json.Unmarshal failed: %w", err)
		klog.Error(err)
		return "", err
	}
	if authResp.Code != 0 {
		err := fmt.Errorf("authentication failed: %s", authResp.Msg)
		klog.Error(err)
		return "", err
	}
	AccessToken := authResp.Data.AccessToken
	return AccessToken, nil
}

func parseConfigToStruct(data map[string]string, target any) error {
	v := reflect.ValueOf(target)
	if v.Kind() != reflect.Ptr || v.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("target must be a pointer to struct")
	}

	v = v.Elem()
	t := v.Type()

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		configKey := field.Tag.Get("configmap")
		if configKey == "" {
			continue
		}

		valueStr, ok := data[configKey]
		if !ok {
			return fmt.Errorf("missing required config key: %s", configKey)
		}

		fieldValue := v.Field(i)
		if !fieldValue.CanSet() {
			continue
		}

		switch fieldValue.Kind() {
		case reflect.Int:
			val, err := strconv.Atoi(valueStr)
			if err != nil {
				return fmt.Errorf("failed to parse %s as int: %w", configKey, err)
			}
			fieldValue.SetInt(int64(val))
		case reflect.Float32, reflect.Float64:
			val, err := strconv.ParseFloat(valueStr, fieldValue.Type().Bits())
			if err != nil {
				return fmt.Errorf("failed to parse %s as float: %w", configKey, err)
			}
			fieldValue.SetFloat(val)
		case reflect.Bool:
			val, err := strconv.ParseBool(valueStr)
			if err != nil {
				return fmt.Errorf("failed to parse %s as bool: %w", configKey, err)
			}
			fieldValue.SetBool(val)
		case reflect.String:
			fieldValue.SetString(valueStr)
		default:
			return fmt.Errorf("unsupported field type: %s", fieldValue.Kind())
		}
	}

	return nil
}

// parseStructToConfig 将结构体转换为 ConfigMap 数据格式
// source 必须是结构体指针或结构体值
// 返回 map[string]string，其中 key 为 configmap 标签的值，value 为字段值的字符串表示
func parseStructToConfig(source any) (map[string]string, error) {
	v := reflect.ValueOf(source)

	// 如果是指针，获取其指向的值
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return nil, fmt.Errorf("source is nil pointer")
		}
		v = v.Elem()
	}

	if v.Kind() != reflect.Struct {
		return nil, fmt.Errorf("source must be a struct or pointer to struct")
	}

	t := v.Type()
	data := make(map[string]string)

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		configKey := field.Tag.Get("configmap")
		if configKey == "" {
			continue
		}

		fieldValue := v.Field(i)

		// 跳过不可访问的字段
		if !fieldValue.CanInterface() {
			continue
		}

		var valueStr string
		switch fieldValue.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			valueStr = strconv.FormatInt(fieldValue.Int(), 10)
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			valueStr = strconv.FormatUint(fieldValue.Uint(), 10)
		case reflect.Float32, reflect.Float64:
			valueStr = strconv.FormatFloat(fieldValue.Float(), 'f', -1, fieldValue.Type().Bits())
		case reflect.Bool:
			valueStr = strconv.FormatBool(fieldValue.Bool())
		case reflect.String:
			valueStr = fieldValue.String()
		default:
			return nil, fmt.Errorf("unsupported field type for field %s: %s", field.Name, fieldValue.Kind())
		}

		data[configKey] = valueStr
	}

	return data, nil
}

func ObjectMapper(src map[string]any, dest any) error {
	data, err := json.Marshal(src)
	if err != nil {
		return fmt.Errorf("json.Marshal failed: %w", err)
	}
	if err := json.Unmarshal(data, dest); err != nil {
		return fmt.Errorf("json.Unmarshal failed: %w", err)
	}
	return nil
}
