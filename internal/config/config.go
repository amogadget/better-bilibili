package config

import (
	"os"
	"sync"

	"gopkg.in/yaml.v3"
)

type Cookies struct {
	SESSDATA   string `yaml:"sessdata"`
	BiliJct    string `yaml:"bili_jct"`
	DedeUserID string `yaml:"dede_user_id"`
}

type Config struct {
	Listen  string  `yaml:"listen"`
	Cookies Cookies `yaml:"cookies"`

	path string
	mu   sync.RWMutex
}

func Load(path string) (*Config, error) {
	c := &Config{path: path, Listen: "127.0.0.1:8765"}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return c, nil
		}
		return nil, err
	}
	if err := yaml.Unmarshal(b, c); err != nil {
		return nil, err
	}
	if c.Listen == "" {
		c.Listen = "127.0.0.1:8765"
	}
	return c, nil
}

func (c *Config) GetCookies() Cookies {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Cookies
}

func (c *Config) SetCookies(ck Cookies) error {
	c.mu.Lock()
	c.Cookies = ck
	c.mu.Unlock()
	return c.save()
}

func (c *Config) save() error {
	c.mu.RLock()
	out := struct {
		Listen  string  `yaml:"listen"`
		Cookies Cookies `yaml:"cookies"`
	}{Listen: c.Listen, Cookies: c.Cookies}
	c.mu.RUnlock()

	b, err := yaml.Marshal(out)
	if err != nil {
		return err
	}
	return os.WriteFile(c.path, b, 0600)
}

func (c *Config) LoggedIn() bool {
	ck := c.GetCookies()
	return ck.SESSDATA != ""
}
