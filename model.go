package main

type CreateSnapshotRequest struct {
	DashboardId string `json:"dashboard_id"`
	Query       string `json:"query"`
	From        int    `json:"from"`
	To          int    `json:"to"`
}

type LoginAndCreateSnapshotRequest struct {
	GrafanaURL  string `json:"grafana_url"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	DashboardId string `json:"dashboard_id"`
	Query       string `json:"query"`
	From        int    `json:"from"`
	To          int    `json:"to"`
}
