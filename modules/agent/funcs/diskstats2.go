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
	logrus.Debug("DiskFailureMetrics")
	dsList, err := nux.ListDiskStats()
	if err != nil {
		logrus.Errorf("DiskFailureMetrics:%s", err)
		return
	}

	rrmap := make(map[string]uint64)
	for _, ds := range dsList {
		if !ShouldHandleDevice(ds.Device) {
			continue
		}
		rrmap[ds.Device] = ds.ReadRequests
	}
	logrus.Debugf("DiskFailureMetrics,disk.io.read_request:%v", rrmap)
	psLock.RLock()
	defer psLock.RUnlock()
	for device := range diskStatsMap {
		if !ShouldHandleDevice(device) {
			continue
		}
		use := IODelta(device, IOMsecTotal)
		duration := IODelta(device, TS)
		if _, ok := rrmap[device]; !ok {
			logrus.Errorln("DiskFailureMetrics get read_requests fail")
			return
		}
		f := float64(use) * 100.0 / float64(duration)
		logrus.Debugf("DiskFailureMetrics device:%s;disk.io.util:%f;CpuIowait:%f;", device, f, CpuIowait())
		if f >= 100.0 && CpuIowait() >= 0.2 && rrmap[device] == 0 {
			logrus.Debug("DiskFailureMetrics:1")
			L = append(L, GaugeValue("disk.failure", 1, "device="+device))
		} else {
			logrus.Debug("DiskFailureMetrics:0")
			L = append(L, GaugeValue("disk.failure", 0, "device="+device))
		}
	}
	return
}

// DiskUtilMax .
func DiskUtilMax() float64 {
	psLock.RLock()
	defer psLock.RUnlock()
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
