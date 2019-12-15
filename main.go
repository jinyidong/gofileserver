/**
 * @Author: Administrator
 * @Description:文件服务器
 * @File:  main
 * @Version: 1.0.0
 * @Date: 2019/12/14 21:03
 */

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"gofileserver/pkg"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"runtime"
)

type fileServerConfig struct {
	Dir        string
	Port       uint16
	DeviceName string //网卡
	FilterRule string
}

var (
	fileServerCfg fileServerConfig
	confPath      string
)

//TODO:初始化文件目录
func init() {
	var tomlPath string
	if runtime.GOOS == `windows` {
		tomlPath = "./gofileserver.toml"
	} else {
		tomlPath = "/config/gofileserver.toml"
	}
	flag.StringVar(&confPath, "conf", tomlPath, "config path")

	viper.SetConfigFile(confPath)
	if err := viper.ReadInConfig(); err != nil {
		panic(err)
	}

	if err := viper.Unmarshal(&fileServerCfg); err != nil {
		panic(err)
	}
}

type response struct {
	Code    int         `json:"Code"`
	Data    interface{} `json:"Data"`
	Message string      `json:"Message"`
}

func main() {
	go pkg.WireShark(fileServerCfg.Port, fileServerCfg.DeviceName, fileServerCfg.FilterRule)

	http.HandleFunc(fileServerCfg.FilterRule, func(w http.ResponseWriter, r *http.Request) {
		filePath := fileServerCfg.Dir + r.URL.Path[1:]
		log.Infof("Download Url:%v", filePath)
		var fileStat os.FileInfo
		var err error
		if fileStat, err = os.Stat(filePath); nil != err { //未查询到文件
			http.NotFoundHandler().ServeHTTP(w, r)
			return
		}
		if fileStat.IsDir() {
			http.NotFoundHandler().ServeHTTP(w, r)
			return
		}
		pkg.SetFileSize(fileStat.Name(), fileStat.Size())
		http.ServeFile(w, r, filePath)
	})

	http.HandleFunc("/bindUdidAndFile", func(w http.ResponseWriter, r *http.Request) {
		body, _ := ioutil.ReadAll(r.Body)
		defer r.Body.Close()

		req := struct {
			UdId     string `json:"udid"`
			FileName string `json:"fileName"`
		}{}

		if err := json.Unmarshal(body, &req); err == nil {
			log.Info(req)
		} else {
			fmt.Fprint(w, makeResponse(-1, nil, err.Error()))
			return
		}

		if req.UdId == "" || req.FileName == "" {
			fmt.Fprint(w, makeResponse(-1, nil, "udid或fileName不能为空！"))
			return
		}

		pkg.BindUdIdAndFile(req.UdId, req.FileName)

		fmt.Fprint(w, makeResponse(0, nil, "success"))
	})

	http.HandleFunc("/getDownloading", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			fmt.Fprint(w, makeResponse(-1, nil, "接口请求方式错误"))
			return
		}
		u, _ := url.Parse(r.URL.String())
		paras, _ := url.ParseQuery(u.RawQuery)
		if paras["udid"][0] == "" {
			fmt.Fprint(w, makeResponse(-1, nil, "参数udid不为空"))
			return
		}
		downloading := pkg.GetDownloading(paras["udid"][0])

		fmt.Fprint(w, makeResponse(0, downloading, "success"))
	})

	http.ListenAndServe(fmt.Sprintf(":%d", fileServerCfg.Port), nil)
}

func makeResponse(code int, data interface{}, msg string) string {
	var resp response
	resp.Code = code
	resp.Message = msg
	resp.Data = data
	ret, _ := json.Marshal(&resp)
	fmt.Println(string(ret))
	return string(ret)
}
