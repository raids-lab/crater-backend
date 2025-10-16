package operations

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseConfigToStruct(t *testing.T) {
	tests := []struct {
		name        string
		configData  map[string]string
		targetType  any
		expectError bool
		validate    func(t *testing.T, target any)
	}{
		{
			name: "解析 LongTimeJobConfig",
			configData: map[string]string{
				"batchDays":       "7",
				"interactiveDays": "3",
			},
			targetType:  &LongTimeJobConfig{},
			expectError: false,
			validate: func(t *testing.T, target any) {
				config := target.(*LongTimeJobConfig)
				assert.Equal(t, 7, config.BatchDays)
				assert.Equal(t, 3, config.InteractiveDays)
			},
		},
		{
			name: "解析 LowGPUUtilJobConfig",
			configData: map[string]string{
				"timeRange": "24",
				"waitTime":  "48",
				"util":      "10",
			},
			targetType:  &LowGPUUtilJobConfig{},
			expectError: false,
			validate: func(t *testing.T, target any) {
				config := target.(*LowGPUUtilJobConfig)
				assert.Equal(t, 24, config.TimeRange)
				assert.Equal(t, 48, config.WaitTime)
				assert.Equal(t, 10, config.Util)
			},
		},
		{
			name: "解析 WaitingJupyterConfig",
			configData: map[string]string{
				"waitMinutes": "30",
			},
			targetType:  &WaitingJupyterConfig{},
			expectError: false,
			validate: func(t *testing.T, target any) {
				config := target.(*WaitingJupyterConfig)
				assert.Equal(t, 30, config.WaitMinutes)
			},
		},
		{
			name: "缺少必需字段",
			configData: map[string]string{
				"batchDays": "7",
			},
			targetType:  &LongTimeJobConfig{},
			expectError: true,
		},
		{
			name: "类型转换错误",
			configData: map[string]string{
				"batchDays":       "invalid_number",
				"interactiveDays": "3",
			},
			targetType:  &LongTimeJobConfig{},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := parseConfigToStruct(tt.configData, tt.targetType)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				if tt.validate != nil {
					tt.validate(t, tt.targetType)
				}
			}
		})
	}
}

type ExampleNewJobConfig struct {
	Threshold int    `configmap:"threshold"`
	Action    string `configmap:"action"`
	MaxCount  int    `configmap:"maxCount"`
}

func TestNewJobConfigParsing(t *testing.T) {
	configData := map[string]string{
		"threshold": "50",
		"action":    "restart",
		"maxCount":  "100",
	}

	config := &ExampleNewJobConfig{}
	err := parseConfigToStruct(configData, config)

	assert.NoError(t, err)
	assert.Equal(t, 50, config.Threshold)
	assert.Equal(t, "restart", config.Action)
	assert.Equal(t, 100, config.MaxCount)
}

func TestParseStructToConfig(t *testing.T) {
	tests := []struct {
		name        string
		source      any
		expected    map[string]string
		expectError bool
	}{
		{
			name: "转换 LongTimeJobConfig",
			source: &LongTimeJobConfig{
				BatchDays:       7,
				InteractiveDays: 3,
			},
			expected: map[string]string{
				"batchDays":       "7",
				"interactiveDays": "3",
			},
			expectError: false,
		},
		{
			name: "转换 LowGPUUtilJobConfig",
			source: &LowGPUUtilJobConfig{
				TimeRange: 24,
				WaitTime:  48,
				Util:      10,
			},
			expected: map[string]string{
				"timeRange": "24",
				"waitTime":  "48",
				"util":      "10",
			},
			expectError: false,
		},
		{
			name: "转换 WaitingJupyterConfig",
			source: &WaitingJupyterConfig{
				WaitMinutes: 30,
			},
			expected: map[string]string{
				"waitMinutes": "30",
			},
			expectError: false,
		},
		{
			name: "转换结构体值（非指针）",
			source: LongTimeJobConfig{
				BatchDays:       5,
				InteractiveDays: 2,
			},
			expected: map[string]string{
				"batchDays":       "5",
				"interactiveDays": "2",
			},
			expectError: false,
		},
		{
			name: "转换 ExampleNewJobConfig",
			source: &ExampleNewJobConfig{
				Threshold: 50,
				Action:    "restart",
				MaxCount:  100,
			},
			expected: map[string]string{
				"threshold": "50",
				"action":    "restart",
				"maxCount":  "100",
			},
			expectError: false,
		},
		{
			name:        "nil 指针应该报错",
			source:      (*LongTimeJobConfig)(nil),
			expected:    nil,
			expectError: true,
		},
		{
			name:        "非结构体类型应该报错",
			source:      "not a struct",
			expected:    nil,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseStructToConfig(tt.source)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestRoundTrip(t *testing.T) {
	tests := []struct {
		name     string
		original any
	}{
		{
			name: "LongTimeJobConfig 往返转换",
			original: &LongTimeJobConfig{
				BatchDays:       7,
				InteractiveDays: 3,
			},
		},
		{
			name: "LowGPUUtilJobConfig 往返转换",
			original: &LowGPUUtilJobConfig{
				TimeRange: 24,
				WaitTime:  48,
				Util:      10,
			},
		},
		{
			name: "WaitingJupyterConfig 往返转换",
			original: &WaitingJupyterConfig{
				WaitMinutes: 30,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configData, err := parseStructToConfig(tt.original)
			assert.NoError(t, err)
			assert.NotNil(t, configData)

			var result any
			switch tt.original.(type) {
			case *LongTimeJobConfig:
				result = &LongTimeJobConfig{}
			case *LowGPUUtilJobConfig:
				result = &LowGPUUtilJobConfig{}
			case *WaitingJupyterConfig:
				result = &WaitingJupyterConfig{}
			}

			err = parseConfigToStruct(configData, result)
			assert.NoError(t, err)

			assert.Equal(t, tt.original, result)
		})
	}
}

func TestParseStructToConfigWithDifferentTypes(t *testing.T) {
	type MixedTypesConfig struct {
		IntValue    int     `configmap:"intValue"`
		FloatValue  float64 `configmap:"floatValue"`
		BoolValue   bool    `configmap:"boolValue"`
		StringValue string  `configmap:"stringValue"`
	}

	source := &MixedTypesConfig{
		IntValue:    42,
		FloatValue:  3.14,
		BoolValue:   true,
		StringValue: "test",
	}

	result, err := parseStructToConfig(source)
	assert.NoError(t, err)
	assert.Equal(t, "42", result["intValue"])
	assert.Equal(t, "3.14", result["floatValue"])
	assert.Equal(t, "true", result["boolValue"])
	assert.Equal(t, "test", result["stringValue"])

	target := &MixedTypesConfig{}
	err = parseConfigToStruct(result, target)
	assert.NoError(t, err)
	assert.Equal(t, source, target)
}
