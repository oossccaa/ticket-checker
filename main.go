package main

import (
	"context"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/joho/godotenv"
)

// 全局變數：預先啟動的瀏覽器
var (
	browserAllocCtx context.Context
	browserCancel   context.CancelFunc
	browserMutex    sync.Mutex // 保護瀏覽器的並發訪問
)

// Config 儲存從環境變數載入的應用程式設定
type Config struct {
	TargetURL     string
	CheckInterval time.Duration // in seconds
}

// loadConfig 從環境變數讀取設定
func loadConfig() (*Config, error) {
	intervalStr := os.Getenv("CHECK_INTERVAL_SECONDS")
	if intervalStr == "" {
		intervalStr = "60" // Default to 60 seconds
	}
	interval, err := strconv.Atoi(intervalStr)
	if err != nil {
		return nil, &configError{"CHECK_INTERVAL_SECONDS 必須是有效的數字"}
	}

	config := &Config{
		TargetURL:     os.Getenv("TARGET_URL"),
		CheckInterval: time.Duration(interval) * time.Second,
	}

	if config.TargetURL == "" {
		return nil, &configError{"環境變數 TARGET_URL 未設定"}
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

// initBrowser 預先初始化瀏覽器
func initBrowser() {
	browserMutex.Lock()
	defer browserMutex.Unlock()

	// 如果已經有瀏覽器在運行，先關閉它
	if browserCancel != nil {
		browserCancel()
	}

	// Chrome 啟動參數（WARP 在系統層級運作，不需要額外設定代理）
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
	)

	browserAllocCtx, browserCancel = chromedp.NewExecAllocator(context.Background(), opts...)
	log.Println("✓ 瀏覽器已預先初始化（系統網路已透過 WARP），隨時待命")
}

// checkTicketAvailability 使用 Headless Chrome 檢查拓元網站上是否有票
func checkTicketAvailability(url string) (bool, error) {
	log.Println("正在使用 Headless Chrome 檢查網址:", url)

	// 使用預先啟動的瀏覽器
	browserMutex.Lock()
	ctx, cancel := chromedp.NewContext(browserAllocCtx)
	browserMutex.Unlock()
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

// autoFillAndWaitForCaptcha 自動填寫表單並等待用戶輸入驗證碼
func autoFillAndWaitForCaptcha(ticketURL string) error {
	log.Println("========== 找到票了！立即打開瀏覽器... ==========")

	// 嘗試多個常見的 Chrome 路徑
	chromePaths := []string{
		"C:\\Program Files (x86)\\Google\\Chrome\\Application\\chrome.exe",
		"C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe",
	}

	var chromePath string
	for _, path := range chromePaths {
		if _, err := os.Stat(path); err == nil {
			chromePath = path
			break
		}
	}

	// 使用系統的 Chrome（WARP 在系統層級運作）
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", false), // 可見模式
		chromedp.Flag("disable-gpu", false),
	)

	if chromePath != "" {
		opts = append(opts, chromedp.ExecPath(chromePath))
		log.Printf("使用 Chrome: %s", chromePath)
	}

	allocCtx, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
	defer cancel()

	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()

	// 增加超時時間，讓用戶有時間輸入驗證碼
	ctx, cancel = context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	log.Println("正在導航到選票頁面...")

	err := chromedp.Run(ctx,
		// 導航到選票頁面
		chromedp.Navigate(ticketURL),
		// 等待表單載入
		chromedp.WaitVisible(`#ticketPriceList`, chromedp.ByQuery),
		chromedp.Sleep(500*time.Millisecond),

		// 使用 JavaScript 自動找到第一個票價 select 並選擇 1 張票
		chromedp.Evaluate(`
			(() => {
				// 找到所有票價選擇器
				const selects = document.querySelectorAll('select[name^="TicketForm[ticketPrice]"]');
				if (selects.length > 0) {
					// 選擇第一個（通常是全票）
					selects[0].value = "1";
					selects[0].dispatchEvent(new Event('change', { bubbles: true }));
					console.log('已選擇 1 張票:', selects[0].id);
					return true;
				}
				return false;
			})()
		`, nil),
		chromedp.Sleep(300*time.Millisecond),

		// 自動勾選同意條款
		chromedp.Click(`#TicketForm_agree`, chromedp.ByQuery),
		chromedp.Sleep(300*time.Millisecond),

		// 將焦點移到驗證碼輸入框
		chromedp.Focus(`#TicketForm_verifyCode`, chromedp.ByQuery),
	)

	if err != nil {
		log.Printf("自動填寫表單時發生錯誤: %v", err)
		return err
	}

	log.Println("=========================================")
	log.Println("已自動完成以下步驟：")
	log.Println("✓ 選擇 1 張票")
	log.Println("✓ 勾選同意條款")
	log.Println("✓ 焦點已移至驗證碼輸入框")
	log.Println("")
	log.Println("請立即輸入驗證碼並點擊【確認張數】按鈕！")
	log.Println("=========================================")

	// 保持瀏覽器開啟，等待用戶操作
	// 這裡可以選擇等待一段時間或直接返回讓程式繼續監控
	time.Sleep(3 * time.Minute) // 給用戶 3 分鐘時間完成購票

	return nil
}

func main() {
	// 在載入設定前，先從 .env 檔案載入環境變數
	err := godotenv.Load()
	if err != nil {
		// 如果 .env 不存在也沒關係，程式會繼續嘗試從系統環境變數讀取
		log.Println("提示: 未找到 .env 檔案，將只從系統環境變數讀取。")
	}

	log.Println("=========================================")
	log.Println("🚀 啟動拓元搶票偵測器...")
	log.Println("=========================================")

	config, err := loadConfig()
	if err != nil {
		log.Fatalf("錯誤: 無法載入設定: %v", err)
	}

	log.Printf("✓ 設定載入成功")
	log.Printf("✓ 監控網址: %s", config.TargetURL)
	log.Printf("✓ 檢查間隔: %v", config.CheckInterval)
	log.Println("=========================================\n")

	// 預先初始化瀏覽器，加快響應速度
	initBrowser()
	defer func() {
		if browserCancel != nil {
			browserCancel()
		}
	}()

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
		log.Println("🎫 偵測到有票！正在啟動自動搶票流程...")

		// 直接打開瀏覽器並自動填寫表單
		if err := autoFillAndWaitForCaptcha(config.TargetURL); err != nil {
			log.Printf("自動填寫失敗: %v", err)
			log.Println("請手動前往:", config.TargetURL)
		} else {
			log.Println("已完成自動填寫，等待您完成購票。")
		}

		// 購票流程完成後，可以選擇結束程式或繼續監控
		log.Println("提示: 如需繼續監控其他場次，請保持程式運行。")
	}
}
