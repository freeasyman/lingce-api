package patientops

import (
	"encoding/json"
	"fmt"
	"reflect"
)

// validateJSONSchema 校验当前截图契约实际用到的 JSON Schema 子集：
// type / required / properties / items / enum / minLength / minItems / anyOf。
// 目的不是实现完整标准，而是让数据库里的 output_schema 真正约束截图输出。
func validateJSONSchema(schemaJSON string, value any) error {
	var schema map[string]any
	if err := json.Unmarshal([]byte(schemaJSON), &schema); err != nil {
		return fmt.Errorf("parse output schema: %w", err)
	}
	return validateSchemaNode(schema, value, "$")
}

func validateSchemaNode(schema map[string]any, value any, path string) error {
	if raw, ok := schema["anyOf"].([]any); ok {
		var last error
		for _, candidate := range raw {
			node, ok := candidate.(map[string]any)
			if !ok {
				continue
			}
			if err := validateSchemaNode(node, value, path); err == nil {
				return nil
			} else {
				last = err
			}
		}
		if last != nil {
			return last
		}
	}
	if rawType, exists := schema["type"]; exists {
		if err := validateType(rawType, value, path); err != nil {
			return err
		}
	}
	if rawEnum, ok := schema["enum"].([]any); ok {
		found := false
		for _, expected := range rawEnum {
			if reflect.DeepEqual(value, expected) {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("%s is not one of the allowed values", path)
		}
	}
	if minLength, ok := schema["minLength"].(float64); ok {
		if text, ok := value.(string); ok && len([]rune(text)) < int(minLength) {
			return fmt.Errorf("%s is shorter than minLength", path)
		}
	}
	if minItems, ok := schema["minItems"].(float64); ok {
		if list, ok := value.([]any); ok && len(list) < int(minItems) {
			return fmt.Errorf("%s has fewer than minItems", path)
		}
	}
	if object, ok := value.(map[string]any); ok {
		if required, ok := schema["required"].([]any); ok {
			for _, raw := range required {
				key, _ := raw.(string)
				if _, exists := object[key]; !exists {
					return fmt.Errorf("%s.%s is required", path, key)
				}
			}
		}
		if properties, ok := schema["properties"].(map[string]any); ok {
			if additional, ok := schema["additionalProperties"].(bool); ok && !additional {
				for key := range object {
					if _, exists := properties[key]; !exists {
						return fmt.Errorf("%s.%s is not allowed", path, key)
					}
				}
			}
			for key, raw := range properties {
				childSchema, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				child, exists := object[key]
				if !exists {
					continue
				}
				if err := validateSchemaNode(childSchema, child, path+"."+key); err != nil {
					return err
				}
			}
		}
	}
	if list, ok := value.([]any); ok {
		if items, ok := schema["items"].(map[string]any); ok {
			for i, item := range list {
				if err := validateSchemaNode(items, item, fmt.Sprintf("%s[%d]", path, i)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func validateType(rawType any, value any, path string) error {
	expected, ok := rawType.(string)
	if !ok {
		return nil
	}
	valid := false
	switch expected {
	case "object":
		_, valid = value.(map[string]any)
	case "array":
		_, valid = value.([]any)
	case "string":
		_, valid = value.(string)
	case "boolean":
		_, valid = value.(bool)
	case "number":
		switch value.(type) {
		case float64, float32, int, int64, int32, json.Number:
			valid = true
		}
	case "integer":
		switch v := value.(type) {
		case float64:
			valid = v == float64(int64(v))
		case int, int64, int32:
			valid = true
		case json.Number:
			_, err := v.Int64()
			valid = err == nil
		}
	case "null":
		valid = value == nil
	default:
		return nil
	}
	if !valid {
		return fmt.Errorf("%s must be %s", path, expected)
	}
	return nil
}
