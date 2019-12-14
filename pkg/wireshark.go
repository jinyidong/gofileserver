package pkg

import (
	"fmt"
	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcap"
	log "github.com/sirupsen/logrus"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

var (
	udIdFileMap sync.Map
)

func BindUdIdAndFile(udId, file string) {
	udIdFileMap.Store(udId, file)
}

func GetDownloading(udId string) int {
	var fileSize, downloadSize int64

	//step1:根据udId获取文件
	iFileName, ok := udIdAndFileMap.Load(udId)
	if !ok {
		log.Warningf("未获取到udId(%s)对应的文件名称", udId)
		return 0
	}
	fileName, ok := iFileName.(string)
	if !ok {
		log.Warningf("udId(%s)对应的文件(%v)类型断言失败", udId, iFileName)
		return 0
	}
	if fileName == "" {
		log.Warningf("udId(%s)对应的文件(%v)名为空", udId, iFileName)
		return 0
	}

	//step1.1:根据文件名获取文件大小
	iFileSize, ok := fileSizeMap.Load(fileName)
	if !ok {
		log.Warningf("未获取到udId(%s)->文件(%s)的大小", udId, fileName)
		return 0
	}
	tempFileSize, ok := iFileSize.(int64)
	if !ok {
		log.Warningf("udId(%s)->文件(%v)所对应的IPPort(%v)类型断言失败", udId, fileName, iFileSize)
		return 0
	}
	fileSize = tempFileSize

	//step2:根据文件名获取ip:port
	iIPPort, ok := fileAndIPPortMap.Load(fileName)
	if !ok {
		log.Warningf("未获取到udId(%s)->文件(%s)的IP:Port", udId, fileName)
		return 0
	}
	ipPort, ok := iIPPort.(string)
	if !ok {
		log.Warningf("udId(%s)->文件(%v)所对应的IPPort(%v)类型断言失败", udId, fileName, iIPPort)
		return 0
	}

	//step3:根据ip:port获取流量
	iTraffic, ok := ipPortTrafficMap.Load(ipPort)
	if !ok {
		log.Warningf("未获取到Udid(%s)->文件(%s)->IP:Port(%s)对应的下载流量", udId, fileName, ipPort)
		return 0
	}
	traffic, ok := iTraffic.(int64)
	if !ok {
		log.Warningf("Udid(%s)->文件(%s)->IPPort(%s)类型所对应的流量(%v)类型断言失败", udId, fileName, ipPort, iTraffic)
		return 0
	}

	//step4:流量统计
	downloadSize = traffic
	log.Infof("download size:%v,file size:%v", downloadSize, fileSize)
	if fileSize == 0 {
		return 0
	}
	return int(math.Min(float64(downloadSize)/float64(fileSize)*100, 100))
}

var (
	snapshotLen      = int32(65536)
	promiscuous      = true
	timeout          = pcap.BlockForever
	udIdAndFileMap   sync.Map
	fileAndIPPortMap sync.Map
	ipPortTrafficMap sync.Map
	fileSizeMap      sync.Map
)

//TODO:网络流量抓包监控
func WireShark(watchPort uint16, deviceName string, filterRule string) {
	filter := getFilter(watchPort)
	handle, err := pcap.OpenLive(deviceName, snapshotLen, promiscuous, timeout)
	if err != nil {
		log.Error(err)
		return
	}
	if err := handle.SetBPFFilter(filter); err != nil {
		log.Error(err)
		return
	}
	defer handle.Close()

	packetSource := gopacket.NewPacketSource(handle, handle.LinkType())
	packetSource.NoCopy = true

	for packet := range packetSource.Packets() {
		if packet.NetworkLayer() == nil || packet.TransportLayer() == nil || packet.TransportLayer().LayerType() != layers.LayerTypeTCP {
			continue
		}
		var srcIP, srcPort, dstIP, dstPort string

		ipLayer := packet.Layer(layers.LayerTypeIPv4)
		if ipLayer != nil {
			ip, _ := ipLayer.(*layers.IPv4)
			srcIP = ip.SrcIP.String()
			dstIP = ip.DstIP.String()
		}

		tcpLayer := packet.Layer(layers.LayerTypeTCP)
		if tcpLayer != nil {
			tcp, _ := tcpLayer.(*layers.TCP)
			srcPort = tcp.SrcPort.String()
			dstPort = tcp.DstPort.String()
		}

		applicationLayer := packet.ApplicationLayer()
		if applicationLayer == nil {
			continue
		}
		log.Infof("%v --->  %v", srcIP+"_"+srcPort, dstIP+"_"+dstPort)

		//TODO:入口请求过滤
		if !strings.Contains(srcPort, strconv.Itoa(int(watchPort))) {
			inputPayloadStr := string(applicationLayer.Payload())
			log.Infof("request:%s", inputPayloadStr)
			if match, _ := regexp.MatchString(filterRule, inputPayloadStr); match {
				requests := strings.Split(inputPayloadStr, " ")
				if len(requests) < 2 {
					continue
				}
				u, err := url.Parse(requests[1])
				if nil != err {
					log.Error(err)
					continue
				}
				paths := strings.Split(u.Path, "/")
				fileName := paths[len(paths)-1]
				if "" == fileName {
					log.Errorf("未获取到文件名,%s", u.Path)
					continue
				}
				fileAndIPPortMap.Store(fileName, srcIP+"_"+srcPort)
				ipPortTrafficMap.Store(srcIP+"_"+srcPort, int64(0))
				fileSizeMap.Store(fileName, getFileSize(u.Path))
			}
		}

		//TODO:出口流量统计
		log.Infof("%v --->  %v", srcIP+"_"+srcPort, dstIP+"_"+dstPort)
		key := dstIP + "_" + dstPort
		if v, ok := ipPortTrafficMap.Load(key); ok {
			if vv, ok := v.(int64); ok {
				ipPortTrafficMap.Store(key, vv+int64(len(applicationLayer.Payload())))
				log.Infof("iPPortFileMap(key:%v,value:%v)", key, vv+int64(len(applicationLayer.Payload())))
			}
		} else {
			ipPortTrafficMap.Store(key, int64(len(applicationLayer.Payload())))
			log.Infof("iPPortFileMap(key:%v,value:%v)", key, len(applicationLayer.Payload()))
		}
	}
}

//TODO:定义过滤器
func getFilter(port uint16) string {
	filter := fmt.Sprintf("tcp and ((src port %v) or (dst port %v))", port, port)
	return filter
}

//TODO:获取文件大小
func getFileSize(path string) int64 {
	defer func() {
		if err := recover(); err != nil {
			log.Errorf("getFileSize error:%v", err)
		}
	}()
	var result int64
	filepath.Walk(path, func(path string, f os.FileInfo, err error) error {
		result = f.Size()
		return nil
	})
	log.Infof("getFileSize--->%s ：%d", path, result)
	return result
}
