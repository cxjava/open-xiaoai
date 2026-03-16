package ota

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/idootop/open-xiaoai/client-patch-go/internal/account"
	"github.com/idootop/open-xiaoai/client-patch-go/internal/logger"
	"github.com/idootop/open-xiaoai/client-patch-go/internal/mina"
)

const otaSecret = "8007236f-a2d6-4847-ac83-c49395ad6d65"

var supportedDevices = []string{"LX06", "OH2P"}

type OTAResult struct {
	SN      string
	Model   string
	Version string
	URL     string
}

type FirmwareInfo struct {
	Link      string `json:"link"`
	Hash      string `json:"hash"`
	ToVersion string `json:"toVersion"`
	Size      int64  `json:"size"`
}

type OTAAPIResponse struct {
	Code interface{} `json:"code"` // 可能是 "0" 或 0
	Data struct {
		CurrentInfo *FirmwareInfo `json:"currentInfo"`
	} `json:"data"`
}

func isOTASuccess(code interface{}) bool {
	switch v := code.(type) {
	case string:
		return v == "0"
	case float64:
		return v == 0
	default:
		return false
	}
}

func GetOTA(acc *account.MiAccount, channel string, debugVersion string, client *http.Client) (*OTAResult, error) {
	if channel == "" {
		channel = "release"
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}

	logger.Debug("GetOTA: channel=%s, debugVersion=%s", channel, debugVersion)

	// 1. 登录并获取设备
	acc, err := account.GetAccount(acc, client)
	if err != nil {
		return nil, err
	}

	// 2. 获取设备列表并匹配
	acc, err = mina.GetDevice(acc, client)
	if err != nil {
		return nil, err
	}
	if acc.Device == nil {
		return nil, fmt.Errorf("找不到设备：%s\n🐛 请检查你的 did 与米家中的设备名称是否一致\n💡 建议打开 debug 选项，查看目标设备的真实 name、miotDID 或 mac 地址", acc.DID)
	}

	dev := acc.Device
	if !isSupported(dev.Hardware) {
		return nil, fmt.Errorf("暂不支持当前设备型号: %s（%s）", dev.Hardware, dev.Name)
	}

	model := dev.Hardware
	sn := dev.SerialNumber
	version := dev.RomVersion
	if debugVersion != "" {
		sn = ""
		version = debugVersion
	}

	// 3. 构造 OTA URL
	timeMs := time.Now().UnixMilli()
	otaInfo := fmt.Sprintf("channel=%s&filterID=%s&locale=zh_CN&model=%s&time=%d&version=%s&%s",
		channel, sn, model, timeMs, version, otaSecret)
	base64Str := base64.StdEncoding.EncodeToString([]byte(otaInfo))
	code := md5Hash(base64Str)

	otaURL := fmt.Sprintf("http://api.miwifi.com/rs/grayupgrade/v2/%s?model=%s&version=%s&channel=%s&filterID=%s&locale=zh_CN&time=%d&s=%s",
		model, model, version, channel, sn, timeMs, code)
	logger.Debug("OTA URL: %s", otaURL)

	return &OTAResult{SN: sn, Model: model, Version: version, URL: otaURL}, nil
}

func isSupported(hardware string) bool {
	for _, h := range supportedDevices {
		if h == hardware {
			return true
		}
	}
	return false
}

func md5Hash(s string) string {
	h := md5.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}

func FetchFirmwareInfo(otaURL string, client *http.Client) (*FirmwareInfo, error) {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	logger.Debug("请求 OTA API: %s", otaURL)
	resp, err := client.Get(otaURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result OTAAPIResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	if !isOTASuccess(result.Code) {
		logger.Debug("OTA API 响应: code=%v", result.Code)
		return nil, fmt.Errorf("获取固件信息失败: %v", result.Code)
	}
	logger.Debug("OTA API 成功，获取到固件链接")
	if result.Data.CurrentInfo == nil {
		return nil, fmt.Errorf("获取固件信息失败: 无固件数据")
	}
	return result.Data.CurrentInfo, nil
}

func DownloadFirmware(firmware *FirmwareInfo, assetsDir string, client *http.Client) (string, error) {
	if firmware == nil || firmware.Link == "" {
		return "", fmt.Errorf("无效的固件信息")
	}
	if client == nil {
		client = &http.Client{Timeout: 0} // 下载大文件不设超时
	}

	u, err := url.Parse(firmware.Link)
	if err != nil {
		return "", err
	}
	filename := filepath.Base(u.Path)
	destPath := filepath.Join(assetsDir, filename)

	if err := os.MkdirAll(assetsDir, 0755); err != nil {
		return "", err
	}

	if _, err := os.Stat(destPath); err == nil {
		logger.Info("文件已存在: %s", destPath)
		return destPath, nil
	}

	logger.Info("开始下载: %s", firmware.Link)
	startTime := time.Now()

	resp, err := client.Get(firmware.Link)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("下载失败: %d %s", resp.StatusCode, resp.Status)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	totalSize := resp.ContentLength
	var downloaded int64
	buf := make([]byte, 32*1024)
	lastPercent := -1

	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			written, wErr := f.Write(buf[:n])
			if wErr != nil {
				return "", wErr
			}
			downloaded += int64(written)
			if totalSize > 0 {
				percent := int(float64(downloaded) / float64(totalSize) * 100)
				if percent > lastPercent && percent <= 100 {
					lastPercent = percent
					downloadedMB := float64(downloaded) / 1024 / 1024
					totalMB := float64(totalSize) / 1024 / 1024
					fmt.Printf("\r下载进度: %d%% | %.2fMB/%.2fMB", percent, downloadedMB, totalMB)
				}
			} else if downloaded%(1024*1024) == 0 {
				fmt.Printf("\r已下载: %.2fMB", float64(downloaded)/1024/1024)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
	}

	elapsed := time.Since(startTime).Seconds()
	logger.Info("下载完成: %s (%.2fMB, %.2f秒)", destPath, float64(downloaded)/1024/1024, elapsed)
	return destPath, nil
}

