package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const fileName = ".gatorconfig.json"

type Config struct {
	Db_url		  	string `json:"db_url"`
	CurrentUserName string `json:"current_user_name"`
}

func (c *Config) SetUser(userName string) error {
	c.CurrentUserName = userName

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	configPath := filepath.Join(homeDir, fileName)
	
	file, err := os.Create(configPath)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	err = encoder.Encode(c)
	if err != nil {
		return err
	}
	return nil
}

func Read() (Config, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return Config{}, err
	}

	configPath := filepath.Join(homeDir, fileName)
	data, err := os.Open(configPath)
	if err != nil {
		return Config{}, nil
	}
	defer data.Close()

	decoder := json.NewDecoder(data)
	cfg := Config{}
	err = decoder.Decode(&cfg)
	if err != nil {
		return Config{}, err
	}
	
	return cfg, err
}
