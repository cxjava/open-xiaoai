package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/joho/godotenv"

	"github.com/cxjava/open-xiaoai/tools/client-patch/internal/account"
	"github.com/cxjava/open-xiaoai/tools/client-patch/internal/ota"
)

func main() {
	// 加载 .env
	_ = godotenv.Load()

	miUser := os.Getenv("MI_USER")
	miPass := os.Getenv("MI_PASS")
	miDID := os.Getenv("MI_DID")
	passToken := os.Getenv("MI_TOKEN")
	debugVersion := os.Getenv("DEBUG_VERSION")
	otaEnv := os.Getenv("OTA")

	if miDID == "" {
		fmt.Println("❌ 请设置 MI_DID 环境变量（小爱音箱名称）")
		os.Exit(1)
	}
	if passToken == "" && (miUser == "" || miPass == "") {
		fmt.Println("❌ 请设置 MI_USER/MI_PASS 或 MI_TOKEN 环境变量")
		os.Exit(1)
	}

	acc := &account.MiAccount{
		SID:       "micoapi",
		DeviceID:  "android_" + uuid.New().String(),
		UserID:    miUser,
		Password:  miPass,
		PassToken: passToken,
		DID:       miDID,
	}

	var otaResult *ota.OTAResult
	var err error

	if otaEnv != "" {
		// 从环境变量解析 OTA 信息（用于 CI 等场景）
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(otaEnv), &parsed); err == nil {
			if url, ok := parsed["url"].(string); ok {
				otaResult = &ota.OTAResult{
					URL:     url,
					Model:   getStr(parsed, "model"),
					Version: getStr(parsed, "version"),
					SN:      getStr(parsed, "sn"),
				}
			}
		}
	}
	if otaResult == nil {
		fmt.Println("🔥 [OTA] 正在获取设备信息...")
		otaResult, err = ota.GetOTA(acc, "release", debugVersion, nil)
		if err != nil {
			fmt.Printf("❌ %v\n", err)
			os.Exit(1)
		}
	}

	if otaResult.URL == "" {
		fmt.Println("❌ 获取设备信息失败")
		os.Exit(1)
	}

	if debugVersion != "" {
		fmt.Printf("%+v\n", otaResult)
		return
	}

	fmt.Println("🔥 [OTA] 正在获取 OTA 信息...")
	firmware, err := ota.FetchFirmwareInfo(otaResult.URL, nil)
	if err != nil {
		fmt.Printf("❌ %v\n", err)
		os.Exit(1)
	}

	if firmware == nil {
		fmt.Println("❌ 获取固件信息失败: 无固件数据")
		os.Exit(1)
	}

	fmt.Print("\n[OTA] === 当前版本固件 ===\n\n")
	sizeStr := "未知"
	if firmware.Size > 0 {
		sizeStr = fmt.Sprintf("%.2fMB", float64(firmware.Size)/1024/1024)
	}
	fmt.Printf("- 版本: %s\n", orEmpty(firmware.ToVersion))
	fmt.Printf("- 大小: %s\n", sizeStr)
	fmt.Printf("- 文件: %s\n", filepath.Base(firmware.Link))
	fmt.Printf("- MD5: %s\n\n", orEmpty(firmware.Hash))

	cwd, _ := os.Getwd()
	assetsDir := filepath.Join(cwd, "assets")
	_, err = ota.DownloadFirmware(firmware, assetsDir, nil)
	if err != nil {
		fmt.Printf("❌ %v\n", err)
		os.Exit(1)
	}

	modelFile := filepath.Join(assetsDir, ".model")
	versionFile := filepath.Join(assetsDir, ".version")
	modelUpper := strings.ToUpper(otaResult.Model)
	fmt.Printf("[OTA] 写入元数据: .model=%s, .version=%s\n", modelUpper, otaResult.Version)
	if err := os.WriteFile(modelFile, []byte(modelUpper), 0644); err != nil {
		fmt.Printf("❌ 写入 .model 失败: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(versionFile, []byte(otaResult.Version), 0644); err != nil {
		fmt.Printf("❌ 写入 .version 失败: %v\n", err)
		os.Exit(1)
	}
}

func orEmpty(s string) string {
	if s == "" {
		return "未知"
	}
	return s
}

func getStr(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok && v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
