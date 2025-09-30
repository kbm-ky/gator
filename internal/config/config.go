package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
)

const configFileName = ".gatorconfig.json"

type Config struct {
	DbUrl           string `json:"db_url"`
	CurrentUserName string `json:"current_user_name"`
}

func (c *Config) SetUser(userName string) error {
	c.CurrentUserName = userName
	filePath, err := getConfigFilepath()
	if err != nil {
		return fmt.Errorf("unable to get config file path: %w", err)
	}

	jsonBlob, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("unable to marshal config to JSON: %w", err)
	}

	err = os.WriteFile(filePath, jsonBlob, 0666)
	if err != nil {
		return fmt.Errorf("unable to write config: %w", err)
	}

	return nil
}

func getConfigFilepath() (string, error) {
	homePath, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("unable to get home directory %w", err)
	}

	filePath := path.Join(homePath, configFileName)
	return filePath, nil
}

func Read() (Config, error) {
	filePath, err := getConfigFilepath()
	if err != nil {
		return Config{}, fmt.Errorf("unable to get config file path: %w", err)
	}

	jsonDat, err := os.ReadFile(filePath)
	if err != nil {
		return Config{}, fmt.Errorf("unable to read file %w", err)
	}

	var config Config
	err = json.Unmarshal(jsonDat, &config)
	if err != nil {
		return Config{}, fmt.Errorf("unable to unmarshal config %w", err)
	}
	return config, nil
}
