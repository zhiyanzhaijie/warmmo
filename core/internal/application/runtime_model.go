package application

import "time"

type RuntimeInfo struct {
	Name       string    `json:"name"`
	Version    string    `json:"version"`
	Status     string    `json:"status"`
	GoVersion  string    `json:"goVersion"`
	Platform   string    `json:"platform"`
	ServerTime time.Time `json:"serverTime"`
	RequestID  string    `json:"requestId"`
}
