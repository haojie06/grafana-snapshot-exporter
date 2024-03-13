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
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
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
		DefaultChromeContextOptions = append(DefaultChromeContextOptions, chromedp.WithLogf(log.Printf))
	}
	// a new browser context
	DefaultAllocContext, DefaultAllocContextCancel = createAllocContext(Headless)
	DefaultChromeContext, DefaultChromeContextCancel = chromedp.NewContext(
		DefaultAllocContext,
		DefaultChromeContextOptions...,
	)
}

func main() {
	defer DefaultAllocContextCancel()
	defer DefaultChromeContextCancel()

	// allocate a new browser, if user/pass is set, login to get grafana session
	// todo: reLogin if session expired
	if DefaultGrafanaPassword != "" && DefaultGrafanaUserName != "" {
		loginContext, cancel := context.WithTimeout(DefaultChromeContext, 30*time.Second)
		defer cancel()
		if err := chromedp.Run(loginContext,
			loginGrafanaTasks(DefaultGrafanaURL, DefaultGrafanaUserName, DefaultGrafanaPassword),
		); err != nil {
			log.Fatalf("loginGrafana err: %s\n", err)
		}
	} else {
		if err := chromedp.Run(DefaultChromeContext, chromedp.Tasks{
			chromedp.Navigate("about:blank"),
		}); err != nil {
			log.Fatalf("chromeContext init err: %s\n", err)
		}
	}

	r := gin.Default()
	r.POST("/snapshot", APIKeyCheck(APIKey), CreateSnapshotHandler)
	r.POST("/login_and_snapshot", APIKeyCheck(APIKey), LoginAndCreateSnapshotHandler) // login and create snapshot in a new chrome process, does not share session with default chrome context

	if err := r.Run(Addr); err != nil {
		log.Fatalf("server run error: %s", err)
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

func CreateSnapshotHandler(c *gin.Context) {
	var req CreateSnapshotRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	chromeContext, cancel := chromedp.NewContext(DefaultChromeContext, DefaultChromeContextOptions...)
	defer cancel()
	snapshotContext, cancel := context.WithTimeout(chromeContext, 30*time.Second)
	defer cancel()
	snapshotKey, err := createSnapshot(snapshotContext, req.DashboardId, req.Query, req.From, req.To)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
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
	if err := chromedp.Run(loginContext,
		loginGrafanaTasks(req.GrafanaURL, req.Username, req.Password),
	); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	snapshotContext, cancel := context.WithTimeout(chromeContext, 30*time.Second)
	defer cancel()
	snapshotKey, err := createSnapshot(snapshotContext, req.DashboardId, req.Query, req.From, req.To)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{
		"url": fmt.Sprintf("%s/dashboard/snapshot/%s", DefaultGrafanaURL, snapshotKey),
	})
}

func createSnapshot(ctx context.Context, dashboardId, query string, from, to int) (string, error) {
	var snapshotURLStr, snapshotKey string
	if err := chromedp.Run(ctx,
		createSnapshotTasks(dashboardId, query, from, to),
		chromedp.Value(`#snapshot-url-input`, &snapshotURLStr),
	); err != nil {
		return "", err
	}
	snapshotURL, err := url.Parse(snapshotURLStr)
	if err != nil {
		return "", err
	}
	snapshotKey = path.Base(snapshotURL.Path)
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
				log.Printf("already login, skip login\n")
				return nil
			}
			return chromedp.Run(ctx, chromedp.WaitVisible(`input[name='user']`),
				chromedp.SendKeys(`input[name='user']`, username),
				chromedp.SendKeys(`input[name='password']`, password),
				chromedp.Click(`button[type='submit']`),
				chromedp.WaitReady(`.page-dashboard`))
		}),
	}
}

func createSnapshotTasks(dashboardId, query string, from, to int) chromedp.Tasks {
	return chromedp.Tasks{
		chromedp.Navigate(fmt.Sprintf("%s/d/%s/?from=%d&to=%d&%s", DefaultGrafanaURL, dashboardId, from, to, query)),
		chromedp.WaitVisible(`.page-dashboard`),
		chromedp.WaitVisible(`div[aria-label='Panel loading bar']`),    // wait for all panel loaded (for debug
		chromedp.WaitNotPresent(`div[aria-label='Panel loading bar']`), // wait for all panel loaded
		chromedp.Click(`button[aria-label='Share dashboard']`),
		chromedp.Click(`button[aria-label='Tab Snapshot']`),
		chromedp.Click(`.css-1i88p6p`), // click on dropdown
		chromedp.WaitVisible(`#react-select-2-listbox`),
		chromedp.Click(`#react-select-2-option-1`), // choose 1 hour expire
		chromedp.Click(`//button[span[text()='Local Snapshot']]`, chromedp.BySearch),
		chromedp.WaitVisible(`#snapshot-url-input`),
	}
}

func createAllocContext(headless bool) (context.Context, context.CancelFunc) {
	opts := append(
		chromedp.DefaultExecAllocatorOptions[:],
		chromedp.WindowSize(1920, 1080),
		chromedp.Flag("headless", headless),
	)
	return chromedp.NewExecAllocator(context.Background(), opts...)
}
