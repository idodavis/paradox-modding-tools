package inventory_services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// LoadConfigFromBytes loads configuration from a byte slice
// This is useful for loading embedded configs
func LoadConfigFromBytes(data []byte) (*InventoryConfig, error) {
	var config InventoryConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}
	return &config, nil
}

// LoadConfigFromFile loads configuration from a JSON file
func LoadConfigFromFile(path string) (*InventoryConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config InventoryConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return &config, nil
}

// SaveConfigToFile saves configuration to a JSON file
func SaveConfigToFile(config *InventoryConfig, path string) error {
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// GetObjectTypeConfig returns the configuration for a specific object type
func (c *InventoryConfig) GetObjectTypeConfig(typeName string) (*ObjectTypeConfig, bool) {
	config, exists := c.ObjectTypes[typeName]
	if !exists {
		return nil, false
	}
	return &config, true
}

// GetSupportedTypes returns a list of all configured object type names
func (c *InventoryConfig) GetSupportedTypes() []string {
	types := make([]string, 0, len(c.ObjectTypes))
	for typeName := range c.ObjectTypes {
		types = append(types, typeName)
	}
	return types
}

// ValidateConfig checks that the configuration is valid
func (c *InventoryConfig) ValidateConfig() error {
	if len(c.ObjectTypes) == 0 {
		return fmt.Errorf("configuration has no object types defined")
	}

	for typeName, typeConfig := range c.ObjectTypes {
		if typeConfig.Name == "" {
			return fmt.Errorf("object type %q has no name", typeName)
		}
		if len(typeConfig.Paths) == 0 {
			return fmt.Errorf("object type %q has no paths", typeName)
		}
		if typeConfig.KeyPattern == "" {
			return fmt.Errorf("object type %q has no key pattern", typeName)
		}

		// Validate key pattern
		validPatterns := map[string]bool{
			"numeric":          true,
			"prefixed":         true,
			"namespaced":       true,
			"keyword_prefixed": true,
			"any":              true,
		}
		if !validPatterns[typeConfig.KeyPattern] {
			return fmt.Errorf("object type %q has invalid key pattern %q", typeName, typeConfig.KeyPattern)
		}

		// If prefixed, must have prefixes
		if typeConfig.KeyPattern == "prefixed" && len(typeConfig.KeyPrefixes) == 0 {
			return fmt.Errorf("object type %q uses prefixed pattern but has no prefixes", typeName)
		}

		// If keyword_prefixed, must have keywords
		if typeConfig.KeyPattern == "keyword_prefixed" && len(typeConfig.KeyKeywords) == 0 {
			return fmt.Errorf("object type %q uses keyword_prefixed pattern but has no keywords", typeName)
		}
	}

	return nil
}
