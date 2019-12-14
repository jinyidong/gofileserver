/**
 * @Author: Administrator
 * @Description:文件服务器
 * @File:  main
 * @Version: 1.0.0
 * @Date: 2019/12/14 21:03
 */

package main

import (
	"flag"
	"fmt"
	"github.com/spf13/viper"
	"net/http"
	"runtime"
)

type fileServerConfig struct {
	Location string
	Dir      string
	Port     uint16
}

var (
	fileServerCfg fileServerConfig
	confPath      string
)

//TODO:初始化文件目录
func init() {
	var tomlPath string
	if runtime.GOOS == `windows` {
		tomlPath = "e:/xinxinserver/config/fileServer.toml"
	} else {
		tomlPath = "/config/fileServer.toml"
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
func main() {
	http.HandleFunc(fileServerCfg.Location, func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, r.URL.Path[1:])
	})

	http.ListenAndServe(fmt.Sprintf(":%d", fileServerCfg.Port), nil)
}
