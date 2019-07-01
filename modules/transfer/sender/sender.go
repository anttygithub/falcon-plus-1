// Copyright 2017 Xiaomi, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package sender

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/Sirupsen/logrus"
	backend "github.com/open-falcon/falcon-plus/common/backend_pool"
	cmodel "github.com/open-falcon/falcon-plus/common/model"
	"github.com/open-falcon/falcon-plus/modules/transfer/g"
	"github.com/open-falcon/falcon-plus/modules/transfer/proc"
	rings "github.com/toolkits/consistent/rings"
	nlist "github.com/toolkits/container/list"
)

const (
	DefaultSendQueueMaxSize = 102400 //10.24w
)

// 默认参数
var (
	MinStep int //最小上报周期,单位sec
)

// 服务节点的一致性哈希环
// pk -> node
var (
	JudgeNodeRing *rings.ConsistentHashNodeRing
	GraphNodeRing *rings.ConsistentHashNodeRing
)

// 发送缓存队列
// node -> queue_of_data
var (
	TsdbQueue   *nlist.SafeListLimited
	ImsQueue    *nlist.SafeListLimited
	JudgeQueues = make(map[string]*nlist.SafeListLimited)
	GraphQueues = make(map[string]*nlist.SafeListLimited)
)

// 连接池
// node_address -> connection_pool
var (
	JudgeConnPools     *backend.SafeRpcConnPools
	TsdbConnPoolHelper *backend.TsdbConnPoolHelper
	GraphConnPools     *backend.SafeRpcConnPools
)

// 初始化数据发送服务, 在main函数中调用
func Start() {
	// 初始化默认参数
	MinStep = g.Config().MinStep
	if MinStep < 1 {
		MinStep = 30 //默认30s
	}
	//
	initConnPools()
	initSendQueues()
	initNodeRings()
	// SendTasks依赖基础组件的初始化,要最后启动
	startSendTasks()
	startSenderCron()
	log.Println("send.Start, ok")
}

// 将数据 打入 某个Judge的发送缓存队列, 具体是哪一个Judge 由一致性哈希 决定
func Push2JudgeSendQueue(items []*cmodel.MetaData) {
	for _, item := range items {
		pk := item.PK()
		node, err := JudgeNodeRing.GetNode(pk)
		if err != nil {
			log.Println("E:", err)
			continue
		}

		// align ts
		step := int(item.Step)
		if step < MinStep {
			step = MinStep
		}
		ts := alignTs(item.Timestamp, int64(step))

		judgeItem := &cmodel.JudgeItem{
			Endpoint:  item.Endpoint,
			Metric:    item.Metric,
			Value:     item.Value,
			Timestamp: ts,
			JudgeType: item.CounterType,
			Tags:      item.Tags,
		}
		Q := JudgeQueues[node]
		isSuccess := Q.PushFront(judgeItem)

		// statistics
		if !isSuccess {
			proc.SendToJudgeDropCnt.Incr()
		}
	}
}

// 将数据 打入 某个Graph的发送缓存队列, 具体是哪一个Graph 由一致性哈希 决定
func Push2GraphSendQueue(items []*cmodel.MetaData) {
	cfg := g.Config().Graph

	for _, item := range items {
		graphItem, err := convert2GraphItem(item)
		if err != nil {
			log.Println("E:", err)
			continue
		}
		pk := item.PK()

		// statistics. 为了效率,放到了这里,因此只有graph是enbale时才能trace
		proc.RecvDataTrace.Trace(pk, item)
		proc.RecvDataFilter.Filter(pk, item.Value, item)

		node, err := GraphNodeRing.GetNode(pk)
		if err != nil {
			log.Println("E:", err)
			continue
		}

		cnode := cfg.ClusterList[node]
		errCnt := 0
		for _, addr := range cnode.Addrs {
			Q := GraphQueues[node+addr]
			if !Q.PushFront(graphItem) {
				errCnt += 1
			}
		}

		// statistics
		if errCnt > 0 {
			proc.SendToGraphDropCnt.Incr()
		}
	}
}

// 打到Graph的数据,要根据rrdtool的特定 来限制 step、counterType、timestamp
func convert2GraphItem(d *cmodel.MetaData) (*cmodel.GraphItem, error) {
	item := &cmodel.GraphItem{}

	item.Endpoint = d.Endpoint
	item.Metric = d.Metric
	item.Tags = d.Tags
	item.Timestamp = d.Timestamp
	item.Value = d.Value
	item.Step = int(d.Step)
	if item.Step < MinStep {
		item.Step = MinStep
	}
	item.Heartbeat = item.Step * 2

	if d.CounterType == g.GAUGE {
		item.DsType = d.CounterType
		item.Min = "U"
		item.Max = "U"
	} else if d.CounterType == g.COUNTER {
		item.DsType = g.DERIVE
		item.Min = "0"
		item.Max = "U"
	} else if d.CounterType == g.DERIVE {
		item.DsType = g.DERIVE
		item.Min = "0"
		item.Max = "U"
	} else {
		return item, fmt.Errorf("not_supported_counter_type")
	}

	item.Timestamp = alignTs(item.Timestamp, int64(item.Step)) //item.Timestamp - item.Timestamp%int64(item.Step)

	return item, nil
}

// 将原始数据入到tsdb发送缓存队列
func Push2TsdbSendQueue(items []*cmodel.MetaData) {
	for _, item := range items {
		tsdbItem := convert2TsdbItem(item)
		isSuccess := TsdbQueue.PushFront(tsdbItem)

		if !isSuccess {
			proc.SendToTsdbDropCnt.Incr()
		}
	}
}

// 转化为tsdb格式
func convert2TsdbItem(d *cmodel.MetaData) *cmodel.TsdbItem {
	t := cmodel.TsdbItem{Tags: make(map[string]string)}

	for k, v := range d.Tags {
		t.Tags[k] = v
	}
	t.Tags["endpoint"] = d.Endpoint
	t.Metric = d.Metric
	t.Timestamp = d.Timestamp
	t.Value = d.Value
	return &t
}

func alignTs(ts int64, period int64) int64 {
	return ts - ts%period
}

// ImsSendCache .
var ImsSendCache = make(map[string]CacheData)

// CacheData .
type CacheData struct {
	Time          int64
	LastTimestamp int64
}

//Push2ImsSendQueue 将原始数据插入到ims缓存队列
func Push2ImsSendQueue(items []*cmodel.MetaData) {
	logrus.Debugf("Push2ImsSendQueue,input len:%d", len(items))
	itemsGroup := make(map[string][]*cmodel.MetaData)
	cfg := g.Config()
	for i := 0; i < len(items); i++ {
		if _, ok := cfg.Ims.FalconToIms[items[i].Metric]; !ok {
			continue //只处理需要上报ims的指标
		}
		//处理采集周期>=ims接收周期
		if items[i].Step >= cfg.Ims.Period {
			//每次都需要上报
			key := items[i].Endpoint + fmt.Sprint(items[i].Timestamp)
			itemsGroup[key] = append(itemsGroup[key], items[i]) //加入上报列表
			continue                                            //处理下一条数据，无需记录缓存
		}
		//处理采集周期<ims接收周期数据
		if _, ok := ImsSendCache[items[i].Endpoint+items[i].Metric]; !ok { //未发送过的ip+指标处理
			ImsSendCache[items[i].Endpoint+items[i].Metric] = CacheData{ //更新缓存
				Time:          0,
				LastTimestamp: items[i].Timestamp,
			}
		} else { //已发送过的ip+指标
			if cfg.Ims.Period > ImsSendCache[items[i].Endpoint+items[i].Metric].Time+items[i].Step { //若没超过ims接收周期
				ImsSendCache[items[i].Endpoint+items[i].Metric] = CacheData{ //更新缓存
					Time:          ImsSendCache[items[i].Endpoint+items[i].Metric].Time + items[i].Step,
					LastTimestamp: items[i].Timestamp,
				}
				continue //跳过，本次无需上报
			} else { //若超过ims接收周期
				ImsSendCache[items[i].Endpoint+items[i].Metric] = CacheData{ //更新缓存
					Time:          ImsSendCache[items[i].Endpoint+items[i].Metric].Time + items[i].Step - cfg.Ims.Period,
					LastTimestamp: items[i].Timestamp,
				}
			}
		}
		key := items[i].Endpoint + fmt.Sprint(items[i].Timestamp)
		itemsGroup[key] = append(itemsGroup[key], items[i]) //加入上报列表
	}
	for k, is := range itemsGroup {
		logrus.Debugf("Push2ImsSendQueue,input key:%s", k)
		logrus.Debugf("Push2ImsSendQueue,input len:%d,group:%v", len(is), is)
		ImsItem := convert2ImsItem(is)
		if ImsItem == nil {
			continue
		}
		isSuccess := ImsQueue.PushFront(ImsItem)

		if !isSuccess {
			proc.SendToImsDropCnt.Incr()
		}
	}
}

// 转化为ims格式
func convert2ImsItem(d []*cmodel.MetaData) *cmodel.ImsItem {
	if len(d) == 0 {
		return nil
	}
	t := cmodel.ImsItem{}
	t.Name = d[0].Endpoint
	t.Timestamp = d[0].Timestamp * 1000
	t.Type = getEndpointType(d[0].Endpoint)
	dl := make(map[string]cmodel.Kv)
	cfg := g.Config()
	ftiMap := cfg.Ims.FalconToIms
	for _, m := range d {
		key := m.Metric
		logrus.Debugf("Metric:%v", m)
		if _, ok := ftiMap[key]; ok {
			i := ftiMap[key]
			metricName := i.Name
			objectName := "-"
			v := m.Value
			if i.Tag != "" {
				if _, ok := m.Tags[i.Tag]; ok {
					objectName = m.Tags[i.Tag]
				}
			}
			if i.Expression != "" {
				if strings.Contains(i.Expression, "value*") {
					c := strings.Split(i.Expression, "value*")
					num, err := strconv.Atoi(c[1])
					if err != nil {
						logrus.Errorf("falconToIms config error:%s", i.Expression)
						return nil
					}
					v = v * float64(num)
				} else if strings.Contains(i.Expression, "value/") {
					c := strings.Split(i.Expression, "value/")
					num, err := strconv.Atoi(c[1])
					if err != nil {
						logrus.Errorf("falconToIms config error:%s", i.Expression)
						return nil
					}
					v = v / float64(num)
				}
			}
			if _, ok := dl[metricName]; ok {
				dl[metricName][objectName] = v
			} else {
				kv := make(cmodel.Kv)
				kv[objectName] = v
				dl[metricName] = kv
			}
		}
	}

	if len(dl) == 0 {
		return nil
	}
	t.DataList = dl
	return &t
}

func imssend(items interface{}) error {
	b, err := json.Marshal(items)
	if err != nil {
		return err
	}
	body := bytes.NewBuffer([]byte(b))

	//POST to IMS
	logrus.Debugf("imssend,input:%s", string(b))
	cfg := g.Config()
	logrus.Debugf("imssend,cfg.Ims.Address:%s", cfg.Ims.Address)
	req, _ := http.NewRequest("POST", cfg.Ims.Address, body)
	req.Header.Set("Content-Type", "application/json")
	// req.Host = beego.AppConfig.String("_imsipport")
	client := http.Client{}
	// logrus.Info("Send data is ", b[:200])
	// logrus.Info("Target url is ", cfg.Ims.Address)

	res, err := client.Do(req)
	if err != nil {
		return err
	}

	result, err := ioutil.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		return err
	}
	logrus.Debugf("IMS return result=%v \n", string(result))
	ob := cmodel.RespondFromIms{}
	err = json.Unmarshal(result, &ob)
	if err != nil {
		return fmt.Errorf("IMS返回结果解析失败：%v", string(result))
	}
	if ob.ResultCode != 0 {
		return fmt.Errorf("IMS return error: %v", ob.ResultMsg)
	}
	return nil
}

func getEndpointType(ep string) string {
	if strings.Contains(ep, "-") {
		return "network"
	}
	return "host"
}
