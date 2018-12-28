// Copyright 2018 anttymo

package model

import (
	"fmt"

	"github.com/open-falcon/falcon-plus/common/utils"
)

type ImsItem struct {
	Endpoint  string            `json:"endpoint"`
	Metric    string            `json:"metric"`
	Value     float64           `json:"value"`
	Timestamp int64             `json:"timestamp"`
	JudgeType string            `json:"judgeType"`
	Tags      map[string]string `json:"tags"`
}

func (this *ImsItem) String() string {
	return fmt.Sprintf("<Endpoint:%s, Metric:%s, Value:%f, Timestamp:%d, JudgeType:%s Tags:%v>",
		this.Endpoint,
		this.Metric,
		this.Value,
		this.Timestamp,
		this.JudgeType,
		this.Tags)
}

func (this *ImsItem) PrimaryKey() string {
	return utils.Md5(utils.PK(this.Endpoint, this.Metric, this.Tags))
}
