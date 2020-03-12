package models

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/garyburd/redigo/redis"
	"github.com/open-falcon/falcon-plus/modules/sender/g"
)

// SenderAlarm 用于接收告警信息
type SenderAlarm struct {
	Metric    string `json:"metric"`   //统计纬度
	Endpoint  string `json:"endpoint"` //主机
	Timestamp string `json:"timestamp"`
	Value     string `json:"value"` //代表匹配关键字的字符串
	Type      string `json:"type"`  //只能是COUNTER或者GAUGE二选一，前者表示该数据采集项为计时器类型，后者表示其为原值 (注意大小写)   modify by nic
	Tag       string `json:"tag"`   //一组逗号分割的键值对, 对metric进一步描述和细化, 可以是空字符串. 比如idc=lg，比如service=xbox等，多个tag之间用逗号分割   modify by nic
	Status    string `json:"status"`
	Desc      string `json:"desc"`
	Level     string `json:"level"`
	Object    string `json:"object"` //设备对象，如端口，接口之类
}

const (
	// SenderAlarmStatusOK .
	SenderAlarmStatusOK = "OK"
	// SenderAlarmStatusPROBLEM .
	SenderAlarmStatusPROBLEM = "PROBLEM"
)

// GetObject 用于提取对象
func (s *SenderAlarm) GetObject() {
	s1 := []string{}
	s2 := []string{}
	// VPN类告警
	if strings.Contains(s.Value, "IPSec tunnel:") {
		s1 = strings.Split(s.Value, "IPSec tunnel:")
		s2 = strings.Split(s1[1], " ")
		s.Object = s2[0]
	}
	// SLA类告警
	if strings.Contains(s.Value, "IP SLAs(") {
		s1 = strings.Split(s.Value, "IP SLAs(")
		s2 = strings.Split(s1[1], ")")
		s.Object = s2[0]
	}
	if strings.Contains(s.Value, "IP SLAs(") {
		s1 = strings.Split(s.Value, "IP SLAs(")
		s2 = strings.Split(s1[1], ")")
		s.Object = s2[0]
	}
	if strings.Contains(s.Value, "ip sla ") {
		s1 = strings.Split(s.Value, "ip sla ")
		s2 = strings.Split(s1[1], " ")
		s.Object = s2[0]
	}
	// 端口down类告警
	if strings.Contains(s.Value, "Interface ") {
		s1 = strings.Split(s.Value, "Interface ")
		s2 = strings.Split(s1[1], " ")
		s.Object = s2[0]
	}
	if strings.Contains(s.Value, "interface ") {
		s1 = strings.Split(s.Value, "interface ")
		s2 = strings.Split(s1[1], " ")
		s.Object = s2[0]
	}
	// BGP、OSPF类告警
	if strings.Contains(s.Value, "neighbor ") {
		s1 = strings.Split(s.Value, "neighbor ")
		s2 = strings.Split(s1[1], " ")
		s.Object = "neighbor " + s2[0]
	}
	if strings.Contains(s.Value, "BGP.: ") {
		s1 = strings.Split(s.Value, "BGP.: ")
		s2 = strings.Split(s1[1], " ")
		s.Object = "BGP.: " + s2[0]
	}
	if strings.Contains(s.Value, "Nbr ") {
		s1 = strings.Split(s.Value, "Nbr ")
		s2 = strings.Split(s1[1], " ")
		s.Object = "Nbr " + s2[0]
	}
	if strings.Contains(s.Value, "Neighbor ") {
		s1 = strings.Split(s.Value, "Neighbor ")
		s2 = strings.Split(s1[1], " ")
		s3 := strings.Split(s2[0], "(")
		s.Object = "Neighbor " + s3[0]
	}
	return
}

// Handler .
func (s *SenderAlarm) Handler() {
	s.GetObject()
	if s.Object == "" {
		log.Println("找不到收敛对象，直接发送告警:", s.Value)
		s.send()
		return
	}

	if s.Status == SenderAlarmStatusPROBLEM {
		go s.workerPROBLEM()
	} else if s.Status == SenderAlarmStatusOK {
		go s.workerOK()
	}
}

func (s *SenderAlarm) workerPROBLEM() {
	if g.Config().Timeout == 0 {
		log.Println("等待时间未设置，请检查配置文件")
		return
	}
	timeout := g.Config().Timeout
	key := s.Endpoint + s.Object
	c := g.RedisConnPool.Get()
	value, _ := json.Marshal(s)
	c.Do("SET", key, value)
	log.Println(redis.String(c.Do("GET", key)))
	for i := 0; i < timeout; i++ {
		time.Sleep(time.Second)
		isKeyExist, err := redis.Bool(c.Do("EXISTS", key))
		if err != nil {
			log.Println("error:", err)
		}
		if !isKeyExist {
			return
		}
	}
	go s.send() //若等待时间内没收到恢复告警，则发送告警
	c.Do("DEL", key)
	return
}
func (s *SenderAlarm) workerOK() {
	key := s.Endpoint + s.Object
	c := g.RedisConnPool.Get()
	isKeyExist, err := redis.Bool(c.Do("EXISTS", key))
	if err != nil {
		log.Println("error:", err)
	}
	if isKeyExist {
		c.Do("DEL", key)
		log.Println("告警已恢复，无需发送告警")
	} else {
		s.send() //若缓存中没有该设备对象的记录，则直接发送恢复告警
	}
	return
}

// ReportAlertRespondStruct .
type ReportAlertRespondStruct struct {
	ResultCode int    `json:"resultCode"`
	ResultMsg  string `json:"resultMsg"`
}

func (s *SenderAlarm) send() (o *ReportAlertRespondStruct, err error) {
	defer func() {
		if err != nil {
			log.Println("发送告警失败:", err)
		}
	}()
	// get device name
	nd := &NetworkDevice{}
	nd.ManageIP = s.Endpoint
	conn, err := g.GetDbConn("GetNameByManageIP")
	if err != nil {
		log.Println("[ERROR] make dbConn fail", err)
		return nil, err
	}
	err = nd.GetNameByManageIP(conn)
	if err != nil {
		log.Println("[ERROR] GetNameByManageIP fail", err)
		return nil, err
	}
	m := make(url.Values)
	// subject="网络设备.SNMP告警.fromTern."+desc
	// content="Endpoint:"+endpoint+" Metric:"+metric+" Note:"+desc+" Value:"+value+" Timestamp:"+timestamp
	subject := "网络设备.SNMP告警.fromTern." + s.Desc
	content := "Endpoint:" + s.Endpoint + " Metric:" + s.Metric + " Note:" + s.Desc + " Value:" + s.Value + " Timestamp:" + s.Timestamp

	//  5127,subject,content,2,tos,0,2,result['endpoint']
	//  sub_system_id,title,info,level,reciver,ecc,alertway,alertip

	//  alertway: 2(mail) 1(rtx)
	//     ecc: 0(notoecc) 1(toecc)
	//     level: 1(critical) 2(major) 3(minor) 4(warning) 5(info)
	level := ""
	if s.Status == SenderAlarmStatusPROBLEM {
		switch s.Level {
		case "critical":
			level = "1"
		case "major":
			level = "2"
		case "minor":
			level = "3"
		case "warning":
			level = "4"
		case "info":
			level = "5"
		}
	} else {
		level = "0"
	}
	m["alert_title"] = []string{subject}
	m["sub_system_id"] = []string{strconv.Itoa(5127)}
	m["alert_level"] = []string{level}
	m["alert_info"] = []string{content}
	m["alert_ip"] = []string{s.Endpoint}
	m["to_ecc"] = []string{strconv.Itoa(0)}
	m["alert_way"] = []string{strconv.Itoa(2)}
	m["alert_reciver"] = []string{g.Config().Tos}
	m["ci_type_name"] = []string{"network_device"}
	m["ci_name"] = []string{nd.Name}
	if s.Object != "" {
		m["alert_obj"] = []string{s.Object}
	} else {
		m["alert_obj"] = []string{nd.Name}
	}
	// m["remark_info"] = []string{}
	// m["use_umg_policy"] = []string{}
	sendurl := g.Config().Sendurl
	//把post表单发送给目标服务器
	log.Println("Send alert data =", m, "\n Timestamp = ", time.Now())
	res, err := http.PostForm(sendurl, m)
	if err != nil {
		return
	}
	getresult, err := ioutil.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		return
	}

	o = &ReportAlertRespondStruct{}
	err = json.Unmarshal(getresult, &o)
	if err != nil {
		err = fmt.Errorf("返回结果解析失败：%v", string(getresult))
		return
	}
	if o.ResultCode != 0 {
		err = fmt.Errorf(o.ResultMsg)
		return
	}
	log.Println("Respond for Send alert :", o)
	return
}
