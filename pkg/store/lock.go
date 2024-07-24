package store

import (
	"os"
	"path"

	"github.com/alexflint/go-filemutex"
)

// newFileLock 创建一个新的文件锁
func newFileLock(lockPath string) (*filemutex.FileMutex, error) {
	// 如果lockPath是一个目录，则将其更改为 lockPath/lock
	file, err := os.Stat(lockPath)
	if err != nil {
		return nil, err
	}

	if file.IsDir() {
		lockPath = path.Join(lockPath, "lock")
	}

	// 创建文件锁
	f, err := filemutex.New(lockPath)
	if err != nil {
		return nil, err
	}

	// 返回文件锁
	return f, nil
}
