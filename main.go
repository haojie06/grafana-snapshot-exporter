package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"path"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"go.uber.org/zap"
)

var (
	Addr   string
	APIKey string

	Headless  bool
	ChromeLog bool

	DefaultAllocContext       context.Context // specify the chrome settings
	DefaultAllocContextCancel context.CancelFunc

	DefaultChromeContextOptions []func(*chromedp.Context)
	DefaultChromeContext        context.Context // a chrome window, share cookies, cache, etc
	DefaultChromeContextCancel  context.CancelFunc

	DefaultGrafanaURL      string
	DefaultGrafanaUserName string
	DefaultGrafanaPassword string
)

func init() {
	err := godotenv.Load()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Fatalf("Error loading .env file: %s", err)
	}
	Addr = os.Getenv("ADDR")
	APIKey = os.Getenv("API_KEY")

	Headless = os.Getenv("HEADLESS") == "true"
	ChromeLog = os.Getenv("CHROME_LOG") == "true"
	DefaultGrafanaURL = os.Getenv("GRAFANA_URL")
	DefaultGrafanaUserName = os.Getenv("GRAFANA_USERNAME")
	DefaultGrafanaPassword = os.Getenv("GRAFANA_PASSWORD")
	if ChromeLog {
		DefaultChromeContextOptions = append(DefaultChromeContextOptions, chromedp.WithLogf(zap.S().Debugf))
	}
	logger, err := zap.NewDevelopment()
	if err != nil {
		log.Fatalf("zap.NewProduction err: %s", err)
	}
	zap.ReplaceGlobals(logger)
	gin.DefaultWriter = zap.NewStdLog(logger).Writer()
	// a new browser context
	DefaultAllocContext, DefaultAllocContextCancel = createAllocContext(Headless)
	DefaultChromeContext, DefaultChromeContextCancel = chromedp.NewContext(
		DefaultAllocContext,
		DefaultChromeContextOptions...,
	)
	if err := chromedp.Run(DefaultChromeContext, chromedp.Tasks{
		chromedp.Navigate("about:blank"),
	}); err != nil {
		zap.S().Fatalf("chromeContext init err: %s", err)
	}
}

func main() {
	defer DefaultAllocContextCancel()
	defer DefaultChromeContextCancel()

	// allocate a new browser, if user/pass is set, login to get grafana session
	// todo: reLogin if session expired
	if DefaultGrafanaURL != "" && DefaultGrafanaPassword != "" && DefaultGrafanaUserName != "" {
		zap.S().Infof("login to grafana: %s", DefaultGrafanaURL)
		loginContext, cancel := context.WithTimeout(DefaultChromeContext, 30*time.Second)
		defer cancel()
		if err := chromedp.Run(loginContext,
			loginGrafanaTasks(DefaultGrafanaURL, DefaultGrafanaUserName, DefaultGrafanaPassword),
		); err != nil {
			zap.S().Fatalf("login to grafana err: %s", err)
		}
		zap.S().Infof("login success")
	} else {
		zap.S().Infof("no grafana username/password set, skip login")
		if err := chromedp.Run(DefaultChromeContext, chromedp.Tasks{
			chromedp.Navigate("about:blank"),
		}); err != nil {
			zap.S().Fatalf("chromeContext init err: %s", err)
		}
		zap.S().Infof("chromeContext init success")
	}

	r := gin.Default()
	r.POST("/snapshot", APIKeyCheck(APIKey), CreateSnapshotHandler)
	r.POST("/login_and_snapshot", APIKeyCheck(APIKey), LoginAndCreateSnapshotHandler) // login and create snapshot in a new chrome process, does not share session with default chrome context
	zap.S().Infof("server run on %s headless: %t", Addr, Headless)
	if err := r.Run(Addr); err != nil {
		zap.S().Fatalf("server run error: %s", err)
	}
}

func APIKeyCheck(key string) gin.HandlerFunc {
	return func(c *gin.Context) {
		apiKeyInReq := c.GetHeader("X-API-KEY")
		if apiKeyInReq != key {
			c.AbortWithStatusJSON(401, gin.H{"error": "unauthorized"})
			return
		}
		c.Next()
	}
}

func TraceIdMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		traceId := uuid.New().String()
		c.Set("traceId", traceId)
		c.Next()
	}
}

func CreateSnapshotHandler(c *gin.Context) {
	if DefaultGrafanaURL == "" || DefaultGrafanaUserName == "" || DefaultGrafanaPassword == "" {
		c.JSON(500, gin.H{"error": "default grafanaURL/username/password not set"})
		return
	}
	var req CreateSnapshotRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	chromeContext, cancel := chromedp.NewContext(DefaultChromeContext)
	defer cancel()
	snapshotContext, cancel := context.WithTimeout(chromeContext, 45*time.Second)
	defer cancel()
	snapshotKey, err := createSnapshot(snapshotContext, req.Name, DefaultGrafanaURL, req.DashboardId, req.Query, req.From, req.To)
	if err != nil {
		if errors.Is(err, ErrDashboardNeedLogin) && DefaultGrafanaURL != "" && DefaultGrafanaUserName != "" && DefaultGrafanaPassword != "" {
			// relogin and retry
			zap.S().Infof("relogin and retry")
			loginContext, cancel := context.WithTimeout(chromeContext, 30*time.Second)
			defer cancel()
			if err := chromedp.Run(loginContext,
				loginGrafanaTasks(DefaultGrafanaURL, DefaultGrafanaUserName, DefaultGrafanaPassword)); err != nil {
				err = fmt.Errorf("relogin error: %w", err)
				c.JSON(500, gin.H{"error": err.Error()})
			}
			snapshotContext, cancel := context.WithTimeout(chromeContext, 45*time.Second)
			defer cancel()
			snapshotKey, err = createSnapshot(snapshotContext, req.Name, DefaultGrafanaURL, req.DashboardId, req.Query, req.From, req.To)
			if err != nil {
				err = fmt.Errorf("retry create snapshot error: %w", err)
				c.JSON(500, gin.H{"error": err.Error()})
				return
			}
		} else {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
	}
	c.JSON(200, gin.H{
		"url": fmt.Sprintf("%s/dashboard/snapshot/%s", DefaultGrafanaURL, snapshotKey),
	})
}

// use new chrome context to login and create snapshot
func LoginAndCreateSnapshotHandler(c *gin.Context) {
	var req LoginAndCreateSnapshotRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	allocContext, cancel := createAllocContext(Headless) // create new chrome process
	defer cancel()
	chromeContext, cancel := chromedp.NewContext(allocContext, DefaultChromeContextOptions...)
	defer cancel()
	loginContext, cancel := context.WithTimeout(chromeContext, 30*time.Second)
	defer cancel()

	zap.S().Infof("login to grafana: %s", req.GrafanaURL)
	if err := chromedp.Run(loginContext,
		loginGrafanaTasks(req.GrafanaURL, req.Username, req.Password),
	); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	zap.S().Infof("login success")

	snapshotContext, cancel := context.WithTimeout(chromeContext, 45*time.Second)
	defer cancel()
	snapshotKey, err := createSnapshot(snapshotContext, req.Name, req.GrafanaURL, req.DashboardId, req.Query, req.From, req.To)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{
		"url": fmt.Sprintf("%s/dashboard/snapshot/%s", DefaultGrafanaURL, snapshotKey),
	})
}

func createSnapshot(ctx context.Context, snapshotName, grafanaURL, dashboardId, query string, from, to int) (string, error) {
	zap.S().Debugf("start creating snapshot: dashboardId: %s, query: %s, from: %d, to: %d", dashboardId, query, from, to)
	var snapshotURLStr, snapshotKey string
	if err := chromedp.Run(ctx,
		createSnapshotTasks(snapshotName, grafanaURL, dashboardId, query, from, to),
		chromedp.Value(`#snapshot-url-input`, &snapshotURLStr),
	); err != nil {
		return "", err
	}
	snapshotURL, err := url.Parse(snapshotURLStr)
	if err != nil {
		return "", err
	}
	snapshotKey = path.Base(snapshotURL.Path)
	zap.S().Infof("create snapshot success: %s", snapshotKey)
	return snapshotKey, nil
}

func loginGrafanaTasks(grfanaURL, username, password string) chromedp.Tasks {
	return chromedp.Tasks{
		chromedp.EmulateViewport(1920, 1080),
		chromedp.Navigate(fmt.Sprintf("%s/login", grfanaURL)),
		chromedp.ActionFunc(func(ctx context.Context) error {
			// if already login, skip login
			var currentLocation string
			if err := chromedp.Run(ctx,
				chromedp.WaitReady(`body`),
				chromedp.Location(&currentLocation),
			); err != nil {
				return err
			}
			locationBase := path.Base(currentLocation)
			if locationBase != "login" {
				zap.S().Infof("already login, skip login")
				return nil
			}
			return chromedp.Run(ctx, chromedp.WaitReady(`input[name='user']`),
				chromedp.SendKeys(`input[name='user']`, username),
				chromedp.SendKeys(`input[name='password']`, password),
				chromedp.Click(`button[type='submit']`),
				chromedp.WaitReady(`.page-dashboard`))
		}),
	}
}

func logAction(msg string) chromedp.ActionFunc {
	return func(ctx context.Context) error {
		zap.S().Debug(msg)
		return nil
	}
}

func createSnapshotTasks(snapshotName, grafanaURL, dashboardId, query string, from, to int) chromedp.Tasks {
	var multiBackspace string
	for i := 0; i < 20; i++ {
		multiBackspace += kb.Backspace
	}
	return chromedp.Tasks{
		chromedp.Navigate(fmt.Sprintf("%s/d/%s/?from=%d&to=%d&%s", grafanaURL, dashboardId, from, to, query)),
		logAction("wait for dashboard loaded"),
		chromedp.ActionFunc(func(ctx context.Context) error {
			var screenshot []byte
			if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
				if err := chromedp.Run(ctx,
					chromedp.Sleep(5*time.Second),
					chromedp.FullScreenshot(&screenshot, 100)); err != nil {
					return err
				}
				if err := os.WriteFile("screenshot.png", screenshot, 0644); err != nil {
					zap.S().Errorf("write screenshot")
				}
				return nil
			})); err != nil {
				return err
			}
			// check if need login
			var currentLocation string
			if err := chromedp.Run(ctx,
				logAction("wait for body loaded"),
				chromedp.WaitReady(`body`),
				logAction("get current location"),
				chromedp.Location(&currentLocation),
			); err != nil {
				return err
			}
			locationBase := path.Base(currentLocation)
			if locationBase == "login" {
				return ErrDashboardNeedLogin
			}
			return nil
		}),
		logAction("dashboard loaded, wait for panel loaded"),
		chromedp.WaitReady(`div[aria-label='Panel loading bar']`),      // wait for all panel loaded (for debug
		chromedp.WaitNotPresent(`div[aria-label='Panel loading bar']`), // wait for all panel loaded
		logAction("all panel loaded"),
		chromedp.Click(`button[aria-label='Share dashboard']`),
		chromedp.Click(`button[aria-label='Tab Snapshot']`),
		chromedp.WaitReady(`#snapshot-name-input`),
		logAction("click on snapshot name input"),
		chromedp.Click(`#snapshot-name-input`, chromedp.ByID),
		chromedp.KeyEvent(kb.End),
		chromedp.KeyEvent(multiBackspace),
		chromedp.KeyEvent(kb.Backspace),
		chromedp.SendKeys(`#snapshot-name-input`, snapshotName),
		chromedp.Click(`.css-1i88p6p`), // click on dropdown
		logAction("click on dropdown"),
		chromedp.WaitReady(`#react-select-2-listbox`),
		chromedp.Click(`#react-select-2-option-1`), // choose 1 hour expire
		chromedp.Click(`//button[span[text()='Local Snapshot']]`, chromedp.BySearch),
		logAction("click on local snapshot"),
		chromedp.WaitReady(`#snapshot-url-input`),
	}
}

func createAllocContext(headless bool) (context.Context, context.CancelFunc) {
	opts := append(
		chromedp.DefaultExecAllocatorOptions[:],
		chromedp.WindowSize(1920, 1080),
		chromedp.Flag("headless", headless),
		chromedp.Flag("disable-gpu", headless),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
	)
	return chromedp.NewExecAllocator(context.Background(), opts...)
}
