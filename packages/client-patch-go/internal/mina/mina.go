package mina

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/idootop/open-xiaoai/client-patch-go/internal/account"
	"github.com/idootop/open-xiaoai/client-patch-go/internal/logger"
)

const minaAPI = "https://api2.mina.mi.com"
const minaUserAgent = "MICO/AndroidApp/@SHIP.TO.2A2FE0D7@/2.4.40"

type deviceListResponse struct {
	Code int          `json:"code"`
	Data []MiNADevice `json:"data"`
}

type MiNADevice struct {
	DeviceID        string `json:"deviceID"`
	SerialNumber    string `json:"serialNumber"`
	Name            string `json:"name"`
	Alias           string `json:"alias"`
	MiotDID         string `json:"miotDID"`
	Hardware        string `json:"hardware"`
	DeviceSNProfile string `json:"deviceSNProfile"`
	Mac             string `json:"mac"`
	RomVersion      string `json:"romVersion"`
}

func GetDevice(acc *account.MiAccount, client *http.Client) (*account.MiAccount, error) {
	if acc.SID != "micoapi" {
		return acc, nil
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}

	logger.Debug("请求 MiNA device_list, did=%s", acc.DID)
	devices, err := callMiNA(acc, "GET", "/admin/v2/device_list", nil, client)
	if err != nil {
		return nil, err
	}

	var deviceList []interface{}
	switch v := devices.(type) {
	case []interface{}:
		deviceList = v
	case map[string]interface{}:
		if list, ok := v["list"].([]interface{}); ok {
			deviceList = list
		}
	}
	logger.Debug("device_list 返回 %d 个设备", len(deviceList))

	for _, d := range deviceList {
		dm, ok := d.(map[string]interface{})
		if !ok {
			continue
		}
		deviceID := getStr(dm, "deviceID")
		miotDID := getStr(dm, "miotDID")
		name := getStr(dm, "name")
		alias := getStr(dm, "alias")
		mac := getStr(dm, "mac")

		if matchDevice(acc.DID, deviceID, miotDID, name, alias, mac) {
			logger.Debug("匹配到设备: %s (hardware=%s, romVersion=%s)", name, getStr(dm, "hardware"), getStr(dm, "romVersion"))
			acc.Device = &account.MiNADevice{
				DeviceID:        deviceID,
				SerialNumber:    getStr(dm, "serialNumber"),
				Name:            name,
				Alias:           alias,
				MiotDID:         miotDID,
				Hardware:        getStr(dm, "hardware"),
				DeviceSNProfile: getStr(dm, "deviceSNProfile"),
				Mac:             mac,
				RomVersion:      getStr(dm, "romVersion"),
			}
			return acc, nil
		}
	}
	logger.Debug("未找到匹配设备，共 %d 个设备", len(deviceList))
	return acc, nil
}

func matchDevice(did, deviceID, miotDID, name, alias, mac string) bool {
	candidates := []string{deviceID, miotDID, name, alias, mac}
	for _, c := range candidates {
		if c == did {
			return true
		}
	}
	return false
}

func getStr(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok && v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func callMiNA(acc *account.MiAccount, method, path string, reqData map[string]interface{}, client *http.Client) (interface{}, error) {
	if reqData == nil {
		reqData = make(map[string]interface{})
	}
	reqData["requestId"] = uuid.New().String()
	reqData["timestamp"] = time.Now().Unix()

	var req *http.Request
	var err error

	apiURL := minaAPI + path
	if method == "GET" {
		params := url.Values{}
		for k, v := range reqData {
			if v != nil && v != "" {
				params.Set(k, fmt.Sprint(v))
			}
		}
		if acc.Device != nil {
			if acc.Device.SerialNumber != "" {
				params.Set("sn", acc.Device.SerialNumber)
			}
			if acc.Device.Hardware != "" {
				params.Set("hardware", acc.Device.Hardware)
			}
			if acc.Device.DeviceID != "" {
				params.Set("deviceId", acc.Device.DeviceID)
			}
			if acc.Device.DeviceSNProfile != "" {
				params.Set("deviceSNProfile", acc.Device.DeviceSNProfile)
			}
		}
		fullURL := apiURL + "?" + params.Encode()
		req, err = http.NewRequest("GET", fullURL, nil)
	} else {
		form := url.Values{}
		for k, v := range reqData {
			if v != nil && v != "" {
				form.Set(k, fmt.Sprint(v))
			}
		}
		req, err = http.NewRequest("POST", apiURL, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", minaUserAgent)
	req.Header.Set("Cookie", buildMiNACookie(acc))

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	if result.Code != 0 {
		logger.Debug("MiNA API 错误: code=%d, body=%s", result.Code, string(body))
		return nil, fmt.Errorf("MiNA API 返回错误: code=%d", result.Code)
	}

	var respData interface{}
	if err := json.Unmarshal(result.Data, &respData); err != nil {
		return nil, err
	}
	return respData, nil
}

func buildMiNACookie(acc *account.MiAccount) string {
	parts := []string{
		"userId=" + acc.UserID,
		"serviceToken=" + acc.ServiceToken,
	}
	if acc.Device != nil {
		if acc.Device.SerialNumber != "" {
			parts = append(parts, "sn="+url.QueryEscape(acc.Device.SerialNumber))
		}
		if acc.Device.Hardware != "" {
			parts = append(parts, "hardware="+url.QueryEscape(acc.Device.Hardware))
		}
		if acc.Device.DeviceID != "" {
			parts = append(parts, "deviceId="+url.QueryEscape(acc.Device.DeviceID))
		}
		if acc.Device.DeviceSNProfile != "" {
			parts = append(parts, "deviceSNProfile="+url.QueryEscape(acc.Device.DeviceSNProfile))
		}
	}
	return strings.Join(parts, "; ")
}
