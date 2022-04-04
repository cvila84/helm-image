package credentials

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
)

type auth struct {
	Auth string `json:"auth"`
}

type dockerConfig struct {
	Auths map[string]auth `json:"auths"`
}

func GetAuth(repo string) (string, string, error) {
	login, password, err := GetAuthFromVault(repo)
	if err != nil || len(login) == 0 || len(password) == 0 {
		login, password, err = getAuthFromDockerConfig(repo)
	}
	return login, password, err
}

func getAuthFromDockerConfig(repo string) (string, string, error) {
	usr, err := user.Current()
	if err != nil {
		return "", "", err
	}
	configPath := filepath.FromSlash(usr.HomeDir + "/.docker")
	configFilePath := filepath.FromSlash(configPath + "/config.json")
	file, err := os.Open(configFilePath)
	defer file.Close()
	if err != nil {
		return "", "", err
	}
	decoder := json.NewDecoder(file)
	var config dockerConfig
	err = decoder.Decode(&config)
	if err != nil {
		return "", "", err
	}
	if config.Auths != nil {
		if _, ok := config.Auths[repo]; ok {
			uDec, err := base64.URLEncoding.DecodeString(config.Auths[repo].Auth)
			if err != nil {
				return "", "", err
			}
			authTokens := strings.Split(string(uDec), ":")
			if len(authTokens) != 2 {
				return "", "", fmt.Errorf("invalid authentication information for %s in docker configuration", repo)
			}
			return authTokens[0], authTokens[1], nil
		}
	}
	return "", "", fmt.Errorf("no authentication information found for %s in docker configuration", repo)
}
