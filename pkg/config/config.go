package config

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/containernetworking/cni/pkg/types"
)

const (
	// DefaultSubnetFile 是默认的子网配置文件路径
	DefaultSubnetFile = "/run/raccoon/subnet.json"
	// DefaultBridgeName 是默认的网桥名称
	DefaultBridgeName = "cni0"
)

// SubnetConfig 是子网配置结构体
type SubnetConfig struct {
	Subnet string `json:"subnet"`
	Bridge string `json:"bridge"`
}

// RuntimeConfig 是运行时配置结构体
type RuntimeConfig struct {
	Config map[string]interface{} `json:"config"`
}

// Args 是插件参数结构体
type Args struct {
	Cni map[string]interface{} `json:"cni"`
}

// PluginConfig 是插件配置结构体
type PluginConfig struct {
	types.NetConf
	RuntimeConfig *RuntimeConfig `json:"runtimeConfig,omitempty"`
	Args          *Args          `json:"args"`
	DataDir       string         `json:"dataDir"`
}

// CNIConfig 是CNI配置结构体
type CNIConfig struct {
	PluginConfig
	SubnetConfig
}

// LoadSubnetConfig 从文件中加载子网配置
func LoadSubnetConfig() (*SubnetConfig, error) {
	data, err := os.ReadFile(DefaultSubnetFile)
	if err != nil {
		return nil, err
	}

	c := &SubnetConfig{}
	if err = json.Unmarshal(data, c); err != nil {
		return nil, err
	}

	return c, nil
}

// LoadCNIConfig 从文件中加载CNI配置
func LoadCNIConfig(stdin []byte) (*CNIConfig, error) {
	pluginConf, err := parsePluginConfig(stdin)
	if err != nil {
		return nil, err
	}

	subnetConf, err := LoadSubnetConfig()
	if err != nil {
		return nil, err
	}

	return &CNIConfig{*pluginConf, *subnetConf}, nil
}

// StoreSubnetConfig 存储子网配置到文件
func StoreSubnetConfig(c *SubnetConfig) error {
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}

	return os.WriteFile(DefaultSubnetFile, data, 0644)
}

// parsePluginConfig 解析插件配置
func parsePluginConfig(stdin []byte) (*PluginConfig, error) {
	c := &PluginConfig{}

	if err := json.Unmarshal(stdin, c); err != nil {
		return nil, fmt.Errorf("failed to parse network configuration: %v", err)
	}

	return c, nil
}
