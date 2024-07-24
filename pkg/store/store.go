package store

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"

	"github.com/alexflint/go-filemutex"
)

const (
	defaultDataDir = "/var/lib/cni" // 默认的存储目录
)

// containerNetInfo 存储容器网络信息
type containerNetInfo struct {
	ID     string `json:"id"`     // 容器ID
	IfName string `json:"ifName"` // 容器网卡名称
}

// Data 存储所有容器网络信息
type Data struct {
	Ips  map[string]containerNetInfo `json:"ips"`  // 存储容器网络信息
	Last string                      `json:"last"` // 存储最后一次使用的容器ID
}

// Store 存储器
type Store struct {
	*filemutex.FileMutex        // 文件锁
	dir                  string // 存储目录
	data                 *Data  // 存储数据
	dataFile             string // 存储文件路径
}

// NewStore 创建一个新的存储器
func NewStore(dataDir, network string) (*Store, error) {
	// 如果 dataDir 为空，则使用默认的存储目录
	if dataDir == "" {
		dataDir = defaultDataDir
	}

	// 拼接存储目录路径
	dir := filepath.Join(dataDir, network)
	// 判断存储目录是否存在，不存在则创建
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, err
		}
	}

	// 创建文件锁
	fileLock, err := newFileLock(dir)
	if err != nil {
		return nil, err
	}

	// 获取存储文件路径
	dataFile := filepath.Join(dir, network+".json")

	// 创建存储数据
	data := &Data{Ips: make(map[string]containerNetInfo)}

	// 返回存储器
	return &Store{FileMutex: fileLock, dir: dir, data: data, dataFile: dataFile}, nil
}

// LocalData 获取本地存储数据
func (s *Store) LocalData() error {
	// 创建空数据
	data := &Data{}

	// 读取存储文件, 如果文件不存在，则返回空数据
	raw, err := os.ReadFile(s.dataFile)
	if err != nil {
		if os.IsNotExist(err) {
			f, err := os.Create(s.dataFile)
			if err != nil {
				return err
			}
			defer f.Close()

			_, err = f.Write([]byte("{}"))
			if err != nil {
				return err
			}
		} else {
			return err
		}
	} else {
		if err := json.Unmarshal(raw, &data); err != nil {
			return err
		}
	}

	if data.Ips == nil {
		data.Ips = make(map[string]containerNetInfo)
	}

	s.data = data

	return nil
}

// Last 最新使用的容器 ID
func (s *Store) Last() net.IP {
	// 解析 IP 地址
	return net.ParseIP(s.data.Last)
}

// GetIPByContainerID 根据容器 ID 获取 IP 地址
func (s *Store) GetIPByContainerID(id string) (net.IP, bool) {
	for ip, info := range s.data.Ips {
		if info.ID == id {
			return net.ParseIP(ip), true
		}
	}

	return nil, false
}

// Add 添加 IP 地址和容器信息
func (s *Store) Add(ip net.IP, id, ifName string) error {
	if len(ip) > 0 {
		s.data.Ips[ip.String()] = containerNetInfo{id, ifName} // 添加 IP 地址和容器信息
		s.data.Last = ip.String()                              // 更新最后一次使用的容器 ID

		return s.Store() // 存储数据
	}

	return nil
}

// Del 删除 IP 地址和容器信息
func (s *Store) Del(id string) error {
	for ip, info := range s.data.Ips {
		if info.ID == id {
			delete(s.data.Ips, ip) // 删除 IP 地址和容器信息

			return s.Store() // 存储数据
		}
	}

	return nil
}

// Contain 判断是否包含 IP 地址
func (s *Store) Contain(ip net.IP) bool {
	_, ok := s.data.Ips[ip.String()]
	return ok
}

// Store 存储数据
func (s *Store) Store() error {
	raw, err := json.Marshal(s.data)
	if err != nil {
		return err
	}

	// 写入存储文件
	return os.WriteFile(s.dataFile, raw, 0644)
}
