package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const (
	cmlinkLoginURL = "https://global.cmlink.com/api/login"
	cmlinkAPIURL   = "https://global.cmlink.com/api/user-login/ApiGetGws"
	hk3MsisdnURL   = "https://www.three.com.hk/account-pro/sim/getMsisdnByIccid"
	hk3UserInfoURL = "https://www.three.com.hk/account-pro/user/existingCustDipping"
	holaflyAPIURL  = "https://customers-api.holafly.com/api/customerCard/getByIccid/"
)

type LoginResponse struct {
	Content string `json:"content"`
}

type TokenResponse struct {
	AccessToken string `json:"accessToken"`
}

type UserDataBundle struct {
	Name          string `json:"name"`
	BundleDesc    string `json:"bundleDesc"`
	CreateTime    string `json:"createTime"`
	EndTime       string `json:"endTime"`
	ActiveTime    string `json:"activeTime"`
	ExpireTime    string `json:"expireTime"`
	UsageFlow     string `json:"usageFlow"`
	RemainFlow    string `json:"remainFlow"`
	RemainTime    string `json:"remainTime"`
	RemainingDays string `json:"remainingDays"`
	UsedData      string `json:"usedData"`
	TotalDataMb   string `json:"totalDataMb"`
}

func main() {
	telegramBotToken := flag.String("token", "", "Telegram Bot Token")
	flag.Parse()

	if *telegramBotToken == "" {
		log.Fatal("Telegram Bot Token is required. Use -token flag to provide it.")
	}

	bot, err := tgbotapi.NewBotAPI(*telegramBotToken)
	if err != nil {
		log.Panic(err)
	}

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := bot.GetUpdatesChan(u)

	for update := range updates {
		if update.Message != nil {
			message := update.Message.Text
			if strings.HasPrefix(message, "89") && isNumeric(message) {
				if strings.HasPrefix(message, "8985234") {
					result, keyboard := processCMLink(message)
					msg := tgbotapi.NewMessage(update.Message.Chat.ID, result)
					msg.ReplyMarkup = keyboard
					msg.ParseMode = "HTML"
					bot.Send(msg)
				} else if strings.HasPrefix(message, "8985203") {
					result, _ := processHK3(message)
					msg := tgbotapi.NewMessage(update.Message.Chat.ID, result)
					msg.ParseMode = "HTML"
					bot.Send(msg)
				} else {
					result := processHolafly(message)
					msg := tgbotapi.NewMessage(update.Message.Chat.ID, result)
					msg.ParseMode = "HTML"
					bot.Send(msg)
				}
			} else {
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, "请输入正确格式的 ICCID（以\"89\"开头的数字串）。")
				bot.Send(msg)
			}
		}
	}
}

func processCMLink(iccid string) (string, tgbotapi.InlineKeyboardMarkup) {
	// Step 1: Login to get bearer
	loginData := map[string]string{
		"code":      "sugsqwer1725003007000",
		"password":  "4ea9060a6201c04035b9d7bb3a34d4c5",
		"timestamp": "1725003007000",
	}
	loginResp, err := http.Post(cmlinkLoginURL, "application/json", toJSON(loginData))
	if err != nil {
		log.Println("Error logging in to CMLink:", err)
		return "登录 CMLink 时出错", tgbotapi.InlineKeyboardMarkup{}
	}
	defer loginResp.Body.Close()

	var loginResult LoginResponse
	json.NewDecoder(loginResp.Body).Decode(&loginResult)
	bearer := loginResult.Content

	// Step 2: Get token
	headers := map[string]string{
		"Authorization": "Bearer " + bearer,
	}
	tokenData := map[string]string{
		"url_type": "APP_getAccessToken_SBO",
		"json":     `{"id":"8618922393096","type":104}`,
		"method":   "https",
	}
	tokenResp, err := postRequest(cmlinkAPIURL, headers, tokenData)
	if err != nil {
		log.Println("Error getting token from CMLink:", err)
		return "获取 CMLink token 时出错", tgbotapi.InlineKeyboardMarkup{}
	}

	var tokenResult TokenResponse
	json.NewDecoder(tokenResp.Body).Decode(&tokenResult)
	token := tokenResult.AccessToken

	// Step 3: Get user data
	userData := fetchCMLinkData(bearer, token, iccid)
	result := formatCMLinkBasicInfo(iccid, userData)

	// Create keyboard
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Activate", "activate:"+iccid),
			tgbotapi.NewInlineKeyboardButtonData("Usage", "usage:"+iccid),
		),
	)

	return result, keyboard
}

func processHK3(iccid string) (string, tgbotapi.InlineKeyboardMarkup) {
	// Step 1: Get MSISDN and other data
	resp, err := http.Get(fmt.Sprintf("%s?iccid=%s", hk3MsisdnURL, iccid))
	if err != nil {
		log.Println("Error getting MSISDN from HK3:", err)
		return "无法获取 MSISDN，请检查 ICCID 是否正确。", tgbotapi.InlineKeyboardMarkup{}
	}
	defer resp.Body.Close()

	var data1 map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&data1)
	msisdn, ok := data1["msisdn"].(string)
	if !ok {
		return "无法获取 MSISDN，请检查 ICCID 是否正确。", tgbotapi.InlineKeyboardMarkup{}
	}

	// Step 2: Get user info
	payload, _ := json.Marshal(map[string]string{"id": msisdn})
	req, err := http.NewRequest("POST", hk3UserInfoURL, bytes.NewBuffer(payload))
	if err != nil {
		log.Println("Error creating HK3 user info request:", err)
		return "查询 3HK 数据时发生网络错误，请稍后重试。", tgbotapi.InlineKeyboardMarkup{}
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp2, err := client.Do(req)
	if err != nil {
		log.Println("Error getting user info from HK3:", err)
		return "查询 3HK 数据时发生网络错误，请稍后重试。", tgbotapi.InlineKeyboardMarkup{}
	}
	defer resp2.Body.Close()

	var data2 map[string]interface{}
	json.NewDecoder(resp2.Body).Decode(&data2)

	// Format response
	result := formatHK3Response(iccid, data1, data2)
	return result, tgbotapi.InlineKeyboardMarkup{}
}

func processHolafly(iccid string) string {
	resp, err := http.Get(fmt.Sprintf("%s%s?includeProvider=true", holaflyAPIURL, iccid))
	if err != nil {
		log.Println("Error getting data from Holafly:", err)
		return "查询 Holafly 数据时发生网络错误，请稍后重试。"
	}
	defer resp.Body.Close()

	var data map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&data)

	return formatHolaflyResponse(iccid, data)
}

func fetchCMLinkData(bearer, token, iccid string) UserDataBundle {
	headers := map[string]string{
		"Authorization": "Bearer " + bearer,
	}
	dataBundleData := map[string]string{
		"url_type": "APP_getSubedUserDataBundle_SBO",
		"json":     fmt.Sprintf(`{"iccid":"%s","accessToken":"%s","language":"0","beginIndex":0,"count":100}`, iccid, token),
		"method":   "http",
	}
	resp, err := postRequest(cmlinkAPIURL, headers, dataBundleData)
	if err != nil {
		log.Println("Error getting user data from CMLink:", err)
		return UserDataBundle{}
	}
	defer resp.Body.Close()

	var result map[string][]UserDataBundle
	json.NewDecoder(resp.Body).Decode(&result)
	return result["userDataBundles"][0]
}

func formatCMLinkBasicInfo(iccid string, data UserDataBundle) string {
	return fmt.Sprintf(`<b>ICCID:</b> <code>%s</code>
<b>NAME:</b> %s
<b>DESC:</b> %s
<b>CREATE:</b> %s
<b>END:</b> %s`,
		iccid, data.Name, data.BundleDesc, data.CreateTime, data.EndTime)
}

func formatHK3Response(iccid string, data1, data2 map[string]interface{}) string {
	return fmt.Sprintf(`<b>ICCID:</b> <code>%s</code>
<b>NAME:</b> %s - %s (%s - %s)
<b>TYPE:</b> %s
<b>NUMBER:</b> +852 %s
<b>RECHARGE:</b> %s (%s+)
<b>STATUS:</b> %s
<b>EXPIRY:</b> %s`,
		iccid, data1["brand"], data1["subBrand"], data1["tenantId"], data1["salesChannel"],
		data2["serviceType"], data1["msisdn"], data1["rechargeEligibility"], data1["minimumRechargeAmount"],
		data1["status"], data1["subsEndDate"])
}

func formatHolaflyResponse(iccid string, data map[string]interface{}) string {
	return fmt.Sprintf(`<b>ICCID:</b> <code>%s</code>
<b>ORDER:</b> %s
<b>NAME:</b> %s - %s
<b>CREATE:</b> %s
<b>END:</b> %s
<b>ACTIVE:</b> %s
<b>EXPIRE:</b> %s
<b>TIME:</b> %s / %s Day(s)
<b>DATA:</b> %s / %s MB`,
		iccid, data["order_name"], data["destination"].(map[string]interface{})["en"], data["boundle"].(map[string]interface{})["en"],
		data["createdAt"], data["deactivation_date"], data["activationDate"], data["expirationDate"],
		data["remainingDays"], data["totalDays"], data["usedData"], data["totalDataMb"])
}

func postRequest(url string, headers map[string]string, data map[string]string) (*http.Response, error) {
	client := &http.Client{}
	payload, _ := json.Marshal(data)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(payload))
	if err != nil {
		return nil, err
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	return client.Do(req)
}

func toJSON(data interface{}) *bytes.Buffer {
	jsonData, _ := json.Marshal(data)
	return bytes.NewBuffer(jsonData)
}

func isNumeric(str string) bool {
	for _, c := range str {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
