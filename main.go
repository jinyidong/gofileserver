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
	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"gofileserver/pkg"
	"net/http"
	"os"
	"runtime"
	"time"
)

type fileServerConfig struct {
	Dir            string
	FileServerPort uint16
	DeviceName     string //网卡
	FilterRule     string
	HttpServerPort uint16
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

func main() {
	go pkg.WireShark(fileServerCfg.FileServerPort, fileServerCfg.DeviceName, fileServerCfg.FilterRule)

	go func() {
		router := gin.Default()

		router.POST("/bindUdidAndFile", func(c *gin.Context) {
			req := struct {
				UdId     string `json:"udid"`
				FileName string `json:"fileName"`
			}{}
			err := c.BindJSON(&req)
			if nil != err {
				c.JSON(200, gin.H{
					"Code":    -1,
					"Message": err.Error(),
				})
				return
			}
			if req.UdId == "" || req.FileName == "" {
				c.JSON(200, gin.H{
					"Code":    -1,
					"Message": "udid或fileName不能为空！",
				})
				return
			}
			log.Infof("bindUdidAndFile,udid:%s,fileName:%s", req.UdId, req.FileName)
			pkg.BindUdIdAndFile(req.UdId, req.FileName)
			c.JSON(200, gin.H{
				"Code":    0,
				"Message": "success",
			})
		})

		router.POST("/removeDownloading", func(c *gin.Context) {
			req := struct {
				UdId string `json:"udid"`
			}{}
			err := c.BindJSON(&req)
			if nil != err {
				c.JSON(200, gin.H{
					"Code":    -1,
					"Message": err.Error(),
				})
				return
			}
			if req.UdId == "" {
				c.JSON(200, gin.H{
					"Code":    -1,
					"Message": "udid不能为空！",
				})
				return
			}
			log.Infof("removeDownloading,udId:%s", req.UdId)
			pkg.RemoveDownloading(req.UdId)
			c.JSON(200, gin.H{
				"Code":    0,
				"Message": "success",
			})
		})

		//TODO:获取下载进度
		router.GET("/getDownloading", func(c *gin.Context) {
			udId := c.Query("udid")
			if udId == "" {
				c.JSON(200, gin.H{
					"code":    -1,
					"message": "udid不为空！",
					"data":    nil,
				})
				return
			}
			log.Infof("remoteRemoteAddr:%v", c.Request.RemoteAddr)
			downloading := pkg.GetDownloading(udId)
			c.JSON(200, gin.H{
				"code":    0,
				"message": "success",
				"data":    downloading,
			})
		})

		router.Run(fmt.Sprintf(":%d", fileServerCfg.HttpServerPort))
	}()

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

	s := &http.Server{
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		Addr:         fmt.Sprintf(":%d", fileServerCfg.FileServerPort),
	}

	s.ListenAndServe()
}
