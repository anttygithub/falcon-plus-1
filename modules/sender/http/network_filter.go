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

package http

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"

	"github.com/open-falcon/falcon-plus/modules/sender/models"
)

func configInfoRoutes() {
	// e.g. /strategy/lg-dinp-docker01.bj/cpu.idle
	http.HandleFunc("/alarm/upload", func(w http.ResponseWriter, r *http.Request) {
		// urlParam := r.URL.Path[len("/alarm/upload"):]
		body, err := ioutil.ReadAll(r.Body)
		if err != nil {
			fmt.Printf("read body err, %v\n", err)
			RenderJson(w, Dto{Msg: "read body fail", Data: ""})
			return
		}
		log.Println("receive a request:", string(body))
		var a models.SenderAlarm
		if err = json.Unmarshal(body, &a); err != nil {
			fmt.Printf("Unmarshal err, %v\n", err)
			RenderJson(w, Dto{Msg: "Unmarshal fail", Data: string(body)})
			return
		}
		go a.Handler()
		RenderJson(w, Dto{Msg: "success", Data: "已提交后台进行异步处理"})
	})

}
