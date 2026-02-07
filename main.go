package main

import (
	"context"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/joho/godotenv"
	"gopkg.in/gomail.v2"
)

// Config 儲存從環境變數載入的應用程式設定
type Config struct {
	TargetURL      string
	RecipientEmail string
	SenderEmail    string
	SenderPassword string
	SmtpHost       string
	SmtpPort       int
	CheckInterval  time.Duration // in seconds
}

// loadConfig 從環境變數讀取設定
func loadConfig() (*Config, error) {
	portStr := os.Getenv("SMTP_PORT")
	if portStr == "" {
		portStr = "587" // Default SMTP port
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, &configError{"SMTP_PORT 必須是有效的數字"}
	}

	intervalStr := os.Getenv("CHECK_INTERVAL_SECONDS")
	if intervalStr == "" {
		intervalStr = "60" // Default to 60 seconds
	}
	interval, err := strconv.Atoi(intervalStr)
	if err != nil {
		return nil, &configError{"CHECK_INTERVAL_SECONDS 必須是有效的數字"}
	}

	config := &Config{
		TargetURL:      os.Getenv("TARGET_URL"),
		RecipientEmail: os.Getenv("RECIPIENT_EMAIL"),
		SenderEmail:    os.Getenv("SENDER_EMAIL"),
		SenderPassword: strings.ReplaceAll(os.Getenv("SENDER_PASSWORD"), " ", ""),
		SmtpHost:       os.Getenv("SMTP_HOST"),
		SmtpPort:       port,
		CheckInterval:  time.Duration(interval) * time.Second,
	}

	if config.TargetURL == "" {
		return nil, &configError{"環境變數 TARGET_URL 未設定"}
	}
	if config.RecipientEmail == "" {
		return nil, &configError{"環境變數 RECIPIENT_EMAIL 未設定"}
	}
	if config.SenderEmail == "" {
		return nil, &configError{"環境變數 SENDER_EMAIL 未設定"}
	}
	if config.SenderPassword == "" {
		return nil, &configError{"環境變數 SENDER_PASSWORD 未設定 (提示: 如果使用 Gmail，請使用應用程式密碼)"}
	}
	if config.SmtpHost == "" {
		return nil, &configError{"環境變數 SMTP_HOST 未設定 (例如: smtp.gmail.com)"}
	}

	return config, nil
}

// configError 自訂錯誤類型
type configError struct {
	message string
}

func (e *configError) Error() string {
	return e.message
}

// checkTicketAvailability 使用 Headless Chrome 檢查拓元網站上是否有票
func checkTicketAvailability(url string) (bool, error) {
	log.Println("正在使用 Headless Chrome 檢查網址:", url)

	// 設定 Headless Chrome 的選項
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true), // 設定為 false 可看到瀏覽器畫面，方便除錯
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true), // 在某些環境下（如 Docker）需要
	)
	allocCtx, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
	defer cancel()

	// 建立一個新的 chromedp context
	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()

	// 設定一個總體操作的超時時間
	ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var fontTexts []string

	// 執行任務：導航至頁面，等待票區列表載入，然後取得所有 font 元素的文字
	err := chromedp.Run(ctx,
		chromedp.Navigate(url),
		// 等待第一個票區群組出現
		chromedp.WaitVisible(`#group_0`, chromedp.ByQuery),
		// 取得 group_0 到 group_6 中所有 font 元素的文字內容
		chromedp.Evaluate(`
			(() => {
				let texts = [];
				for (let i = 0; i <= 6; i++) {
					let group = document.getElementById('group_' + i);
					if (group) {
						let fonts = group.querySelectorAll('font');
						fonts.forEach(f => texts.push(f.textContent));
					}
				}
				return texts;
			})()
		`, &fontTexts),
	)

	if err != nil {
		if strings.Contains(err.Error(), "context deadline exceeded") {
			log.Println("在指定時間內未找到票區列表。")
			return false, nil
		}
		log.Printf("Headless Chrome 檢查時發生錯誤: %v", err)
		return false, err
	}

	// 檢查是否有任何 font 包含 "剩餘" 或 "熱賣中" 關鍵字
	for _, text := range fontTexts {
		if strings.Contains(text, "剩餘") || strings.Contains(text, "熱賣中") {
			log.Printf("找到有票的區域: %s", text)
			return true, nil
		}
	}

	log.Printf("檢查了 %d 個票區，目前都已售完。", len(fontTexts))
	return false, nil
}

// sendEmailNotification 發送郵件通知
func sendEmailNotification(config *Config) error {
	log.Println("準備發送 Email 通知至:", config.RecipientEmail)

	m := gomail.NewMessage()
	m.SetHeader("From", config.SenderEmail)
	m.SetHeader("To", config.RecipientEmail)
	m.SetHeader("Subject", "【拓元搶票通知】偵測到有票！")
	m.SetBody("text/html", `
		<html>
		<body>
		<h2>偵測到有票的區域！</h2>
		<p>拓元售票網站上偵測到「剩餘」或「熱賣中」的票區，請立即前往搶票！</p>
		<p><strong>網址:</strong> <a href="`+config.TargetURL+`">`+config.TargetURL+`</a></p>
		<p>祝您搶票順利！</p>
		</body>
		</html>
	`)

	d := gomail.NewDialer(config.SmtpHost, config.SmtpPort, config.SenderEmail, config.SenderPassword)

	if err := d.DialAndSend(m); err != nil {
		return err
	}

	log.Println("Email 通知已成功發送！")
	return nil
}

func main() {
	// 在載入設定前，先從 .env 檔案載入環境變數
	err := godotenv.Load()
	if err != nil {
		// 如果 .env 不存在也沒關係，程式會繼續嘗試從系統環境變數讀取
		log.Println("提示: 未找到 .env 檔案，將只從系統環境變數讀取。")
	}

	log.Println("啟動搶票偵測器...")

	config, err := loadConfig()
	if err != nil {
		log.Fatalf("錯誤: 無法載入設定: %v", err)
	}

	log.Printf("設定載入成功。每 %v 檢查一次。", config.CheckInterval)

	// 使用 for-loop 和 Ticker 進行定期檢查
	ticker := time.NewTicker(config.CheckInterval)
	defer ticker.Stop()

	// 立即執行第一次檢查，而不是等待第一個 Ticker 週期
	runCheck(config)

	for range ticker.C {
		runCheck(config)
	}
}

// runCheck 執行一次完整的檢查流程
func runCheck(config *Config) {
	available, err := checkTicketAvailability(config.TargetURL)
	if err != nil {
		log.Printf("檢查時發生錯誤: %v", err)
		return // 發生錯誤，等待下一次
	}

	if available {
		log.Println("找到票了！正在發送通知...")
		if err := sendEmailNotification(config); err != nil {
			log.Printf("發送 Email 時發生錯誤: %v", err)
		} else {
			log.Println("通知已發送，將繼續監控直到手動關閉程式 (Ctrl+C)。")
		}
	}
}
