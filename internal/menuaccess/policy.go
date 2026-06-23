package menuaccess

import "strings"

type MenuDefinition struct {
	Code        string
	FeatureCode string
}

type PolicyItem struct {
	ItemType string
	ItemCode string
	Enabled  bool
}

type PolicyOverride struct {
	ItemType     string
	ItemCode     string
	OverrideMode string
}

// ResolveAllowedMenuCodes expands feature-level policy items into concrete menu codes.
func ResolveAllowedMenuCodes(defs []MenuDefinition, items []PolicyItem, overrides []PolicyOverride) map[string]struct{} {
	explicitMenus := make(map[string]struct{})
	allowedFeatures := make(map[string]struct{})

	for _, item := range items {
		code := normalize(item.ItemCode)
		if code == "" || !item.Enabled {
			continue
		}
		if normalize(item.ItemType) == "menu" {
			explicitMenus[code] = struct{}{}
			continue
		}
		allowedFeatures[code] = struct{}{}
	}

	for _, override := range overrides {
		code := normalize(override.ItemCode)
		if code == "" {
			continue
		}
		mode := normalize(override.OverrideMode)
		targetIsMenu := normalize(override.ItemType) == "menu"
		if targetIsMenu {
			if mode == "allow" {
				explicitMenus[code] = struct{}{}
			} else {
				delete(explicitMenus, code)
			}
			continue
		}
		if mode == "allow" {
			allowedFeatures[code] = struct{}{}
		} else {
			delete(allowedFeatures, code)
		}
	}

	allowedMenus := make(map[string]struct{}, len(explicitMenus))
	for code := range explicitMenus {
		allowedMenus[code] = struct{}{}
	}
	for _, def := range defs {
		menuCode := normalize(def.Code)
		featureCode := normalize(def.FeatureCode)
		if menuCode == "" || featureCode == "" {
			continue
		}
		if _, ok := allowedFeatures[featureCode]; ok {
			allowedMenus[menuCode] = struct{}{}
		}
	}
	return allowedMenus
}

func normalize(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
