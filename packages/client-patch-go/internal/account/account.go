package account

import (
	"crypto/md5"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/idootop/open-xiaoai/client-patch-go/internal/logger"
)

const loginAPI = "https://account.xiaomi.com/pass"
const userAgent = "Dalvik/2.1.0 (Linux; U; Android 10; RMX2111 Build/QP1A.190711.020) APP/xiaomi.mico APPV/2004040 MK/Uk1YMjExMQ== PassportSDK/3.8.3 passport-ui/3.8.3"

type MiPass struct {
	Code            int    `json:"code"`
	QS              string `json:"qs"`
	Sign            string `json:"_sign"`
	Callback        string `json:"callback"`
	Location        string `json:"location"`
	Ssecurity       string `json:"ssecurity"`
	PassToken       string `json:"passToken"`
	Nonce           string `json:"nonce"`
	UserID          string `json:"userId"`
	CUserID         string `json:"cUserId"`
	NotificationURL string `json:"notificationUrl"`
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

type MiAccount struct {
	SID          string      `json:"sid"`
	DeviceID     string      `json:"deviceId"`
	UserID       string      `json:"userId"`
	Password     string      `json:"password"`
	PassToken    string      `json:"passToken"`
	Pass         *MiPass     `json:"pass"`
	ServiceToken string      `json:"serviceToken"`
	DID          string      `json:"did"`
	Device       *MiNADevice `json:"device"`
}

func md5Hash(s string) string {
	h := md5.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}

func sha1Base64(s string) string {
	h := sha1.Sum([]byte(s))
	return base64.StdEncoding.EncodeToString(h[:])
}

func parseAuthPass(res string) *MiPass {
	res = strings.TrimPrefix(res, "&&&START&&&")
	// 把大数字转成字符串避免 JSON 精度问题
	re := regexp.MustCompile(`:(\d{9,})`)
	res = re.ReplaceAllString(res, `:"$1"`)

	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(res), &raw); err != nil {
		return nil
	}
	pass := &MiPass{}
	if v, ok := raw["code"]; ok {
		switch n := v.(type) {
		case float64:
			pass.Code = int(n)
		case int:
			pass.Code = n
		}
	}
	pass.QS = getStrFromMap(raw, "qs")
	pass.Sign = getStrFromMap(raw, "_sign")
	pass.Callback = getStrFromMap(raw, "callback")
	pass.Location = getStrFromMap(raw, "location")
	pass.Ssecurity = getStrFromMap(raw, "ssecurity")
	pass.PassToken = getStrFromMap(raw, "passToken")
	pass.Nonce = getStrFromMap(raw, "nonce")
	pass.UserID = getStrFromMap(raw, "userId")
	pass.CUserID = getStrFromMap(raw, "cUserId")
	pass.NotificationURL = getStrFromMap(raw, "notificationUrl")
	return pass
}

func getStrFromMap(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok && v != nil {
		switch s := v.(type) {
		case string:
			return s
		case float64:
			return fmt.Sprintf("%.0f", s)
		case int:
			return fmt.Sprintf("%d", s)
		}
	}
	return ""
}

func buildCookie(cookies map[string]string) string {
	var parts []string
	for k, v := range cookies {
		if v != "" {
			parts = append(parts, fmt.Sprintf("%s=%s", k, v))
		}
	}
	return strings.Join(parts, "; ")
}

func getLoginCookies(account *MiAccount) map[string]string {
	cookies := map[string]string{
		"userId":   account.UserID,
		"deviceId": account.DeviceID,
	}
	if account.Pass != nil && account.Pass.PassToken != "" {
		cookies["passToken"] = account.Pass.PassToken
	} else if account.PassToken != "" {
		cookies["passToken"] = account.PassToken
	}
	return cookies
}

func GetAccount(account *MiAccount, client *http.Client) (*MiAccount, error) {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}

	logger.Debug("开始小米账号登录, sid=%s, did=%s", account.SID, account.DID)

	// 1. serviceLogin
	reqURL := loginAPI + "/serviceLogin?sid=" + url.QueryEscape(account.SID) + "&_json=true&_locale=zh_CN"
	logger.Debug("请求 serviceLogin: %s", reqURL)
	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Cookie", buildCookie(getLoginCookies(account)))

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("登录请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	resStr := string(body)

	pass := parseAuthPass(resStr)
	if pass == nil {
		return nil, fmt.Errorf("解析登录响应失败")
	}
	logger.Debug("serviceLogin 响应: code=%d, hasLocation=%v, hasNonce=%v", pass.Code, pass.Location != "", pass.Nonce != "")

	// 2. 如需重新认证，调用 serviceLoginAuth2
	if pass.Code != 0 {
		logger.Debug("登录态失效，调用 serviceLoginAuth2 重新认证")
		if account.Password == "" {
			return nil, fmt.Errorf("登录态已失效，请使用账号密码重新登录或更新 passToken")
		}
		authData := url.Values{}
		authData.Set("_json", "true")
		authData.Set("qs", pass.QS)
		authData.Set("sid", account.SID)
		authData.Set("_sign", pass.Sign)
		authData.Set("callback", pass.Callback)
		authData.Set("user", account.UserID)
		authData.Set("hash", strings.ToUpper(md5Hash(account.Password)))

		req2, err := http.NewRequest("POST", loginAPI+"/serviceLoginAuth2", strings.NewReader(authData.Encode()))
		if err != nil {
			return nil, err
		}
		req2.Header.Set("User-Agent", userAgent)
		req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req2.Header.Set("Cookie", buildCookie(getLoginCookies(account)))

		resp2, err := client.Do(req2)
		if err != nil {
			return nil, fmt.Errorf("OAuth2 登录失败: %w", err)
		}
		defer resp2.Body.Close()

		body2, err := io.ReadAll(resp2.Body)
		if err != nil {
			return nil, err
		}
		pass = parseAuthPass(string(body2))
		if pass == nil {
			return nil, fmt.Errorf("解析 OAuth2 响应失败")
		}
	}

	if strings.Contains(pass.NotificationURL, "identity/authStart") {
		return nil, fmt.Errorf("本次登录需要验证码，请使用 passToken 重新登录\n💡 获取 passToken 教程：https://github.com/idootop/migpt-next/issues/4")
	}
	if pass.Location == "" || pass.Nonce == "" || pass.PassToken == "" {
		return nil, fmt.Errorf("登录失败，请检查你的账号密码是否正确")
	}

	// 3. 获取 serviceToken
	logger.Debug("获取 serviceToken, location=%s", pass.Location)
	serviceToken, err := getServiceToken(pass.Location, pass.Nonce, pass.Ssecurity, client)
	if err != nil {
		return nil, err
	}
	logger.Debug("获取 serviceToken 成功")

	account.Pass = pass
	account.ServiceToken = serviceToken

	return account, nil
}

func getServiceToken(location, nonce, ssecurity string, client *http.Client) (string, error) {
	clientSign := sha1Base64("nonce=" + nonce + "&" + ssecurity)
	reqURL := location + "?_userIdNeedEncrypt=true&clientSign=" + url.QueryEscape(clientSign)

	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("获取 serviceToken 请求失败: %w", err)
	}
	defer resp.Body.Close()

	for _, c := range resp.Header.Values("Set-Cookie") {
		if strings.Contains(c, "serviceToken=") {
			parts := strings.Split(c, ";")
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if strings.HasPrefix(p, "serviceToken=") {
					return strings.TrimPrefix(p, "serviceToken="), nil
				}
			}
		}
	}
	return "", fmt.Errorf("获取 Mi Service Token 失败")
}
