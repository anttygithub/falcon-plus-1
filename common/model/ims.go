// Copyright 2018 anttymo

package model

//ImsItem send these data of a metric to IMS
type ImsItem struct {
	Name      string        `json:"name"`
	Type      string        `json:"type"`
	Timestamp int64         `json:"timestamp"`
	DataList  map[string]Kv `json:"dataList"`
}

type Kv map[string]float64

//RespondFromIms IMS respond
type RespondFromIms struct {
	ResultCode int    `json:"resultCode"`
	ResultMsg  string `json:"resultMsg"`
	SystemTime string `json:"systemTime"`
}

const (
	ImsTypeHost    = "host"
	ImsTypeNetwork = "network"
)
