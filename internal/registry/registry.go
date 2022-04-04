package registry

import (
	"bufio"
	"fmt"
	"github.com/containerd/console"
)

type registryCredentials struct {
	login    string
	password string
}

var cache = map[string]*registryCredentials{}

type Credentials func(host string) func(string) (string, string, error)

func prompt(show bool) (string, error) {
	c := console.Current()
	defer c.Reset()
	if !show {
		if err := c.DisableEcho(); err != nil {
			return "", fmt.Errorf("failed to disable echo: %v", err)
		}
	}
	line, _, err := bufio.NewReader(c).ReadLine()
	if err != nil {
		return "", fmt.Errorf("failed to read line: %v", err)
	}
	return string(line), nil
}

func AddAuthRegistry(host string, login string, password string) {
	cache[host] = &registryCredentials{
		login:    login,
		password: password,
	}
}

func ConsoleCredentials(host string) func(string) (string, string, error) {
	if _, ok := cache[host]; ok {
		return func(host string) (string, string, error) {
			var err error
			defer func() {
				if r := recover(); r != nil {
					switch t := r.(type) {
					case string:
						err = fmt.Errorf("cannot authenticate from console: %s", t)
					case error:
						err = t
					default:
						err = fmt.Errorf("cannot authenticate from console")
					}
					fmt.Printf("\nError: %v\n", err)
				}
			}()
			if len((*cache[host]).login) == 0 {
				fmt.Printf("Please authenticate on %s\n", host)
				fmt.Printf("Login: ")
				(*cache[host]).login, err = prompt(true)
				if err != nil {
					return "", "", err
				}
				if len((*cache[host]).password) == 0 {
					fmt.Printf("Password: ")
					(*cache[host]).password, err = prompt(false)
					if err != nil {
						return "", "", err
					}
					fmt.Print("\n")
				}
			}
			return (*cache[host]).login, (*cache[host]).password, err
		}
	}
	return nil
}
