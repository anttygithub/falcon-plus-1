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

package funcs

import (
	"fmt"
	"runtime"

	"github.com/Sirupsen/logrus"
	"github.com/open-falcon/falcon-plus/common/model"
	"github.com/toolkits/nux"
)

// DiskIOUtilMaxMetrics .
func DiskIOUtilMaxMetrics() (L []*model.MetricValue) {
	L = append(L, GaugeValue("disk.io.util.max", DiskUtilMax()))
	return
}

// DiskFailureMetrics .
func DiskFailureMetrics() (L []*model.MetricValue) {
	normal := false
	defer func() {
		if !normal {
			logrus.Debugf("stack:%s", stack())
		}
		if syserr := recover(); syserr != nil {
			logrus.Errorf("DiskFailureMetrics defer err:%s", syserr)
		}
	}()
	dsList, err := nux.ListDiskStats()
	if err != nil {
		logrus.Errorf("DiskFailureMetrics:%s", err)
		return
	}
	logrus.Debugf("dsList:%v", dsList)

	writerequest := make(map[string]uint64)
	for _, ds := range dsList {
		if !ShouldHandleDevice(ds.Device) {
			continue
		}
		writerequest[ds.Device] = ds.WriteRequests
	}
	logrus.Debugf("DiskFailureMetrics,disk.io.write_request:%v", writerequest)
	dsLock.RLock()
	defer dsLock.RUnlock()
	for device := range diskStatsMap {
		if !ShouldHandleDevice(device) {
			continue
		}
		use := IODelta(device, IOMsecTotal)
		duration := IODelta(device, TS)
		if _, ok := writerequest[device]; !ok {
			logrus.Errorln("DiskFailureMetrics get write_requests fail")
			return
		}
		util := float64(use) * 100.0 / float64(duration)
		logrus.Debugf("DiskFailureMetrics device:%s;disk.io.util:%f;CpuIowait:%f;", device, util, CpuIowait())
		if util >= 100.0 && CpuIowait() > 0 && writerequest[device] == 0 {
			logrus.Debug("DiskFailureMetrics:1")
			L = append(L, GaugeValue("disk.failure", 1, "device="+device))
		} else {
			logrus.Debug("DiskFailureMetrics:0")
			L = append(L, GaugeValue("disk.failure", 0, "device="+device))
		}
	}
	normal = true
	return
}

// DiskUtilMax .
func DiskUtilMax() float64 {
	dsLock.RLock()
	defer dsLock.RUnlock()
	var tmp float64
	tmp = 0
	for device := range diskStatsMap {
		if !ShouldHandleDevice(device) {
			continue
		}
		use := IODelta(device, IOMsecTotal)
		duration := IODelta(device, TS)
		f := float64(use) * 100.0 / float64(duration)
		logrus.Debugf("util:%s", fmt.Sprint(f))
		if tmp < f {
			tmp = f
		}
		if tmp > 100.0 {
			tmp = 100.0
		}
	}
	return tmp
}

func stack() string {
	var buf [2 << 10]byte
	return string(buf[:runtime.Stack(buf[:], true)])
}
