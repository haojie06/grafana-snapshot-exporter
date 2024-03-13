package main

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// func TestMain(m *testing.M) {
// 	// init chrome context
// 	code := m.Run()
// 	os.Exit(code)
// }

func TestLogin(t *testing.T) {
	chromeContext, cancel := chromedp.NewContext(DefaultAllocContext)
	defer cancel()
	if err := chromedp.Run(chromeContext, loginGrafanaTasks(DefaultGrafanaURL, DefaultGrafanaUserName, DefaultGrafanaPassword)); err != nil {
		t.Errorf("loginGrafana() error = %s", err)
	}
}

func TestDuplicateLogin(t *testing.T) {
	chromeContext, cancel := chromedp.NewContext(DefaultAllocContext)
	defer cancel()
	if err := chromedp.Run(chromeContext, loginGrafanaTasks(DefaultGrafanaURL, DefaultGrafanaUserName, DefaultGrafanaPassword)); err != nil {
		t.Errorf("loginGrafana() error = %s", err)
	}
	if err := chromedp.Run(chromeContext, loginGrafanaTasks(DefaultGrafanaURL, DefaultGrafanaUserName, DefaultGrafanaPassword)); err != nil {
		t.Errorf("loginGrafana() error = %s", err)
	}
}

func TestLoginAndCreateSnapshot(t *testing.T) {
	chromeContext, cancel := chromedp.NewContext(DefaultAllocContext)
	defer cancel()

	if err := chromedp.Run(chromeContext, loginGrafanaTasks(DefaultGrafanaURL, DefaultGrafanaUserName, DefaultGrafanaPassword)); err != nil {
		t.Errorf("loginGrafana() error = %s", err)
	} else {
		t.Logf("loginGrafana() ok")
	}
	snapshotKey, err := createSnapshot(chromeContext, "b05cf7ef-3094-4192-9471-80e6b403b2d7", "orgId=1&var-group=public-whitelist", 1710172800000, 1710259199000)
	if err != nil {
		t.Errorf("createSnapshot() error = %s", err)
	} else {
		t.Logf("snapshotKey: %s", snapshotKey)
	}
}

// todo: test login when already logged in
